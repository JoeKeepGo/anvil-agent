package lifecycle

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

// incusInstanceState is the documented Incus state-change body for PUT
// /1.0/instances/{name}/state. Only the allowlisted action and force fields
// are ever emitted.
type incusInstanceState struct {
	Action string `json:"action"`
	Force  bool   `json:"force"`
}

// incusInstanceCreate is the documented Incus instance-creation body for POST
// /1.0/instances. The agent fixes the instance type to "virtual-machine"
// (M13 is the VM lifecycle foundation) and only emits allowlisted, validated
// fields. No config keys, devices, profiles, cloud-init, or user data are
// accepted from the request; the root disk device is copied from the default
// profile by the service and only its size is overridden.
// security.secureboot is derived from the backend-controlled SecureBootEnabled
// field; the agent never accepts arbitrary security.* config from callers.
type incusInstanceCreate struct {
	Name    string                       `json:"name"`
	Type    string                       `json:"type"`
	Source  incusImageSource             `json:"source"`
	Config  map[string]string            `json:"config"`
	Devices map[string]map[string]string `json:"devices"`
}

type incusImageSource struct {
	Type  string `json:"type"`
	Alias string `json:"alias"`
}

type createPayload struct {
	Request  CreateInstanceRequest
	RootDisk profileRootDiskDevice
}

// BuildIncusRequest constructs the allowlisted Incus ProxyRequest for a
// lifecycle action and validated payload. It URL-encodes the instance name
// into the Incus path (defense in depth; the name allowlist already forbids
// characters requiring encoding). It returns an error only if the name fails
// validation — callers should have validated already.
func BuildIncusRequest(action Action, name string, payload interface{}) (*incus.ProxyRequest, *Error) {
	if err := ValidateInstanceName(name); err != nil {
		return nil, err
	}
	encoded := url.PathEscape(name)

	switch action {
	case ActionCreate:
		return buildCreate(encoded, payload)
	case ActionStart, ActionStop, ActionRestart:
		return buildState(encoded, action, payload)
	case ActionDelete:
		return buildDelete(encoded), nil
	default:
		return nil, newErr(http.StatusBadRequest, "UNSUPPORTED_ACTION", "unsupported lifecycle action")
	}
}

func buildCreate(encodedName string, payload interface{}) (*incus.ProxyRequest, *Error) {
	create, ok := payload.(createPayload)
	if !ok {
		return nil, newErr(http.StatusInternalServerError, "INTERNAL", "invalid create payload")
	}
	req := create.Request
	if err := ValidateCreate(req); err != nil {
		return nil, err
	}
	// The Incus instance name in the body uses the validated (unescaped) name;
	// the URL path uses the encoded form.
	body := incusInstanceCreate{
		Name:   req.Name,
		Type:   "virtual-machine",
		Source: incusImageSource{Type: "image", Alias: req.Image},
		Config: map[string]string{
			"limits.cpu":          fmt.Sprintf("%d", req.CPUCount),
			"limits.memory":       formatSize(req.MemoryBytes),
			"security.secureboot": formatSecureBoot(req.SecureBootEnabled),
		},
		Devices: map[string]map[string]string{
			create.RootDisk.Name: create.RootDisk.deviceWithSize(formatSize(req.RootDiskBytes)),
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, newErr(http.StatusInternalServerError, "INTERNAL", "marshal create request")
	}
	return &incus.ProxyRequest{
		Method: http.MethodPost,
		Path:   "/1.0/instances",
		Body:   raw,
	}, nil
}

func buildState(encodedName string, action Action, payload interface{}) (*incus.ProxyRequest, *Error) {
	state, ok := payload.(StateRequest)
	if !ok {
		// A nil/empty state payload is allowed (force defaults to false).
		state = StateRequest{}
	}
	body := incusInstanceState{
		Action: string(action),
		Force:  state.Force,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, newErr(http.StatusInternalServerError, "INTERNAL", "marshal state request")
	}
	return &incus.ProxyRequest{
		Method: http.MethodPut,
		Path:   "/1.0/instances/" + encodedName + "/state",
		Body:   raw,
	}, nil
}

func buildDelete(encodedName string) *incus.ProxyRequest {
	return &incus.ProxyRequest{
		Method: http.MethodDelete,
		Path:   "/1.0/instances/" + encodedName,
	}
}

// formatSecureBoot converts the backend-controlled SecureBootEnabled decision
// to the Incus security.secureboot config string. The value comes from the
// backend policy (resolveVmSecureBootEnabled); the agent never derives it
// autonomously or accepts arbitrary security.* config from callers.
func formatSecureBoot(enabled bool) string {
	if enabled {
		return "true"
	}
	return "false"
}
