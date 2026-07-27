package agent

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type StatusResponse struct {
	MachineID         string                `json:"machine_id"`
	MachineName       string                `json:"machine_name"`
	ListenAddress     string                `json:"listen_address"`
	TunnelMode        string                `json:"tunnel_mode"`
	Tunnel            TunnelStatus          `json:"tunnel"`
	Registration      RegistrationStatus    `json:"registration"`
	Capabilities      Capabilities          `json:"capabilities"`
	PublicURL         string                `json:"public_url,omitempty"`
	AcceptingBuilds   bool                  `json:"accepting_builds"`
	CIAcceptingBuilds bool                  `json:"ci_accepting_builds"`
	ActiveBuilds      int                   `json:"active_builds"`
	QueuedBuilds      int                   `json:"queued_builds"`
	QueuedBuildLimit  int                   `json:"queued_build_limit"`
	Jobs              []string              `json:"jobs"`
	RecentBuilds      []BuildStatusResponse `json:"recent_builds"`
}

type AvailabilityRequest struct {
	AcceptingBuilds *bool `json:"accepting_builds"`
}

type AvailabilityResponse struct {
	AcceptingBuilds bool `json:"accepting_builds"`
}

func (agent *Agent) health(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(response).Encode(map[string]string{"status": "ok"})
}

func (agent *Agent) status(response http.ResponseWriter, request *http.Request) {
	jobs := make([]string, 0, len(agent.config.Jobs))
	for _, job := range agent.config.Jobs {
		jobs = append(jobs, job.ID)
	}
	tunnelStatus := agent.tunnel.Status()
	buildLoad := agent.buildLoad()

	writeJSON(response, http.StatusOK, StatusResponse{
		MachineID:         agent.config.MachineID,
		MachineName:       agent.config.MachineName,
		ListenAddress:     agent.config.ListenAddress,
		TunnelMode:        agent.config.Tunnel.Mode,
		Tunnel:            tunnelStatus,
		Registration:      agent.registration(),
		Capabilities:      agent.config.Capabilities,
		PublicURL:         agent.publicURL(),
		AcceptingBuilds:   agent.acceptingNewBuilds(),
		CIAcceptingBuilds: buildLoad.AcceptingBuilds,
		ActiveBuilds:      buildLoad.ActiveBuilds,
		QueuedBuilds:      buildLoad.QueuedBuilds,
		QueuedBuildLimit:  buildLoad.QueuedBuildLimit,
		Jobs:              jobs,
		RecentBuilds:      agent.recentBuilds(10),
	})
}

func (agent *Agent) setAvailability(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(response, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var availability AvailabilityRequest
	if err := decodeAvailabilityRequest(http.MaxBytesReader(response, request.Body, 1024), &availability); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid availability payload"})
		return
	}
	if availability.AcceptingBuilds == nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "accepting_builds is required"})
		return
	}

	agent.setAcceptingBuilds(*availability.AcceptingBuilds)
	if *availability.AcceptingBuilds {
		Emit(Event{Kind: "registration", Message: "accepting CI builds"})
	} else {
		Emit(Event{Kind: "registration", Message: "paused CI builds"})
	}
	writeJSON(response, http.StatusOK, AvailabilityResponse{AcceptingBuilds: agent.acceptingNewBuilds()})
}

func decodeAvailabilityRequest(reader io.Reader, availability *AvailabilityRequest) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(availability); err != nil {
		return err
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errors.New("availability payload must contain one JSON object")
}

func (agent *Agent) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		header := request.Header.Get("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(agent.config.SharedToken)) != 1 {
			writeJSON(response, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		next(response, request)
	}
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
