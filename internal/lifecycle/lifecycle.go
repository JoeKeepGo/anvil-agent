// Package lifecycle implements the trusted backend-to-agent VM lifecycle
// protocol for anvil-agent. It exposes a narrow, typed, allowlisted set of
// Incus instance operations — create, start, stop, restart, delete — over
// the existing trusted WebSocket transport.
//
// The protocol never accepts arbitrary Incus write paths, never executes
// shell text, never performs snapshots/migration/console/file operations,
// and never echoes raw Incus responses, the Incus Unix socket path, tokens,
// host private config, or product state. Only agent-owned, normalized
// lifecycle fields are returned.
package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

// Action is an allowlisted Incus instance lifecycle action.
type Action string

const (
	ActionCreate  Action = "create"
	ActionStart   Action = "start"
	ActionStop    Action = "stop"
	ActionRestart Action = "restart"
	ActionDelete  Action = "delete"
)

// SupportedActions is the ordered list of actions advertised by the
// capabilities endpoint. It is the single source of truth for what the
// lifecycle protocol will dispatch.
var SupportedActions = []string{
	string(ActionCreate),
	string(ActionStart),
	string(ActionStop),
	string(ActionRestart),
	string(ActionDelete),
}

// Limits keep untrusted request payloads bounded.
const (
	maxBodyBytes     = 1 << 20 // 1 MiB total request body
	maxInstanceLen   = 63      // DNS-label safe cap for Incus instance names
	maxImageLen      = 128     // bounded image alias length
	maxCPUCount      = 1024
	maxMemoryBytes   = 1 << 44 // 16 TiB
	maxRootDiskBytes = 1 << 44 // 16 TiB
)

// CreateInstanceRequest is the body of POST
// /agent/v1/lifecycle/instances/create. Field names mirror the M13 Phase 2
// backend VM lifecycle policy contract (cpuCount/memoryBytes/rootDiskBytes).
type CreateInstanceRequest struct {
	Name          string `json:"name"`
	Image         string `json:"image"`
	CPUCount      int    `json:"cpuCount"`
	MemoryBytes   int64  `json:"memoryBytes"`
	RootDiskBytes int64  `json:"rootDiskBytes"`
}

// StateRequest is the optional body for start/stop/restart. All fields are
// optional and strict-decoded; unknown fields are rejected.
type StateRequest struct {
	Force bool `json:"force"`
}

// DeleteRequest requires an explicit confirmation field. Delete without
// confirm==true is rejected.
type DeleteRequest struct {
	Confirm bool `json:"confirm"`
}

// CapabilitiesResponse is the body of GET
// /agent/v1/lifecycle/capabilities.
type CapabilitiesResponse struct {
	SupportedActions       []string `json:"supportedActions"`
	OperationNormalization bool     `json:"operationNormalization"`
}

// Response is the normalized, agent-owned lifecycle response body. It never
// includes raw Incus output, socket paths, tokens, or product state.
type Response struct {
	Action        Action `json:"action"`
	Instance      string `json:"instance"`
	Status        string `json:"status"`        // agent-owned: "operation-completed" | "sync-ok"
	OperationID   string `json:"operationId"`   // normalized; empty for sync
	OperationKind string `json:"operationKind"` // "async" | "sync"
	ErrorCode     string `json:"errorCode,omitempty"`
	ErrorMessage  string `json:"errorMessage,omitempty"`
}

// Error is a safe lifecycle error. Code is an agent-owned stable reason code;
// Message never echoes submitted values or raw upstream output.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func newErr(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Result is what the proxy converts into an incus.ProxyResponse. On success
// Body is the normalized lifecycle Response and Err is nil. On failure Err is
// set and Body is nil so error responses serialize body:null (matching the
// existing agent error contract).
type Result struct {
	Status int
	Body   json.RawMessage
	Err    *Error
}

// IncusBackend is the narrow executor the lifecycle service uses to reach the
// local Incus Unix socket. It is satisfied by *incus.Client and by test
// fakes.
type IncusBackend interface {
	Execute(context.Context, *incus.ProxyRequest) *incus.ProxyResponse
}

// Service validates and dispatches trusted VM lifecycle requests. It only
// constructs allowlisted Incus requests via BuildIncusRequest and never
// executes shell, accepts arbitrary paths, or performs unsupported ops.
type Service struct {
	incus IncusBackend
}

// NewService returns a lifecycle Service bound to the given Incus backend.
func NewService(backend IncusBackend) *Service {
	return &Service{incus: backend}
}

// Capabilities returns the advertised lifecycle capabilities.
func (s *Service) Capabilities() CapabilitiesResponse {
	return CapabilitiesResponse{
		SupportedActions:       append([]string(nil), SupportedActions...),
		OperationNormalization: true,
	}
}

// Handle dispatches a trusted agent request (method, lifecycle path, body)
// to the allowlisted lifecycle action. Unknown lifecycle paths/methods
// return a safe Error. It never leaks raw Incus output.
func (s *Service) Handle(ctx context.Context, method string, path string, body json.RawMessage) Result {
	if err := ctx.Err(); err != nil {
		return Result{Err: newErr(http.StatusServiceUnavailable, "REQUEST_CANCELLED", "request cancelled")}
	}

	route, ok := parseLifecyclePath(path)
	if !ok {
		return Result{Err: newErr(http.StatusNotFound, "UNKNOWN_LIFECYCLE_PATH", "unknown lifecycle path")}
	}

	switch route.action {
	case ActionCreate:
		return s.handleCreate(ctx, method, body)
	case ActionStart, ActionStop, ActionRestart:
		return s.handleState(ctx, method, route, body)
	case ActionDelete:
		return s.handleDelete(ctx, method, route, body)
	default:
		return Result{Err: newErr(http.StatusNotFound, "UNKNOWN_LIFECYCLE_PATH", "unknown lifecycle path")}
	}
}

func (s *Service) handleCreate(ctx context.Context, method string, body json.RawMessage) Result {
	if method != http.MethodPost {
		return Result{Err: newErr(http.StatusBadRequest, "METHOD_NOT_ALLOWED", "unsupported method for lifecycle create")}
	}
	req, err := decodeCreate(body)
	if err != nil {
		return Result{Err: err}
	}
	rootDisk, err := s.defaultProfileRootDisk(ctx)
	if err != nil {
		return Result{Err: err}
	}
	incusReq, err := BuildIncusRequest(ActionCreate, req.Name, createPayload{
		Request:  req,
		RootDisk: rootDisk,
	})
	if err != nil {
		return Result{Err: err}
	}
	return s.execute(ctx, ActionCreate, req.Name, incusReq)
}

func (s *Service) handleState(ctx context.Context, method string, route lifecycleRoute, body json.RawMessage) Result {
	if method != http.MethodPost {
		return Result{Err: newErr(http.StatusBadRequest, "METHOD_NOT_ALLOWED", "unsupported method for lifecycle action")}
	}
	var state StateRequest
	if len(body) > 0 {
		if err := decodeStrict(body, &state); err != nil {
			return Result{Err: err}
		}
	}
	if err := ValidateInstanceName(route.name); err != nil {
		return Result{Err: err}
	}
	incusReq, err := BuildIncusRequest(route.action, route.name, state)
	if err != nil {
		return Result{Err: err}
	}
	return s.execute(ctx, route.action, route.name, incusReq)
}

func (s *Service) handleDelete(ctx context.Context, method string, route lifecycleRoute, body json.RawMessage) Result {
	if method != http.MethodPost {
		return Result{Err: newErr(http.StatusBadRequest, "METHOD_NOT_ALLOWED", "unsupported method for lifecycle delete")}
	}
	var del DeleteRequest
	if len(body) == 0 {
		return Result{Err: newErr(http.StatusBadRequest, "DELETE_NOT_CONFIRMED", "delete requires explicit confirmation")}
	}
	if err := decodeStrict(body, &del); err != nil {
		return Result{Err: err}
	}
	if !del.Confirm {
		return Result{Err: newErr(http.StatusBadRequest, "DELETE_NOT_CONFIRMED", "delete requires explicit confirmation")}
	}
	if err := ValidateInstanceName(route.name); err != nil {
		return Result{Err: err}
	}
	incusReq, err := BuildIncusRequest(ActionDelete, route.name, del)
	if err != nil {
		return Result{Err: err}
	}
	return s.execute(ctx, ActionDelete, route.name, incusReq)
}

func (s *Service) execute(ctx context.Context, action Action, name string, req *incus.ProxyRequest) Result {
	resp := s.incus.Execute(ctx, req)
	if resp == nil {
		return Result{Err: newErr(http.StatusServiceUnavailable, "INCUS_UNAVAILABLE", "incus lifecycle backend unavailable")}
	}
	return s.normalizeResponse(ctx, action, name, resp)
}

func (s *Service) defaultProfileRootDisk(ctx context.Context) (profileRootDiskDevice, *Error) {
	if err := ctx.Err(); err != nil {
		return profileRootDiskDevice{}, newErr(http.StatusServiceUnavailable, "REQUEST_CANCELLED", "request cancelled")
	}
	resp := s.incus.Execute(ctx, &incus.ProxyRequest{
		Method: http.MethodGet,
		Path:   "/1.0/profiles/default",
	})
	if resp == nil {
		return profileRootDiskDevice{}, newErr(http.StatusServiceUnavailable, "INCUS_PROFILE_UNAVAILABLE", "incus default profile unavailable")
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return profileRootDiskDevice{}, newErr(http.StatusBadGateway, "INCUS_PROFILE_UNAVAILABLE", "incus default profile unavailable")
	}
	return rootDiskFromDefaultProfileResponse(resp)
}

func rootDiskFromDefaultProfileResponse(resp *incus.ProxyResponse) (profileRootDiskDevice, *Error) {
	var env incusProfileEnvelope
	if resp == nil || len(resp.Body) == 0 || json.Unmarshal(resp.Body, &env) != nil || env.Metadata.Devices == nil {
		return profileRootDiskDevice{}, newErr(http.StatusBadGateway, "INCUS_PROFILE_UNAVAILABLE", "incus default profile response is malformed")
	}
	return selectProfileRootDisk(env.Metadata.Devices)
}

// normalizeResponse converts an Incus ProxyResponse into the agent-owned
// lifecycle Response. It never echoes the raw Incus body, socket path, token,
// or product state. Async Incus operations are waited before success is
// returned so operation acceptance is never treated as lifecycle completion.
func (s *Service) normalizeResponse(ctx context.Context, action Action, instance string, resp *incus.ProxyResponse) Result {
	if resp.Status < 200 || resp.Status >= 300 {
		return Result{Err: newErr(resp.Status, mapIncusErrorCode(resp.Status), mapIncusErrorMessage(resp.Status))}
	}

	var env incusEnvelope
	operationID := ""
	operationKind := "sync"
	if len(resp.Body) > 0 {
		// Defensive decode: only the documented Incus REST envelope fields are
		// read. Missing/malformed fields degrade to a sync-ok result; no raw
		// bytes ever escape.
		_ = json.Unmarshal(resp.Body, &env)
		if env.Operation != "" {
			operationKind = "async"
			operationID = normalizeOperationID(env.Operation)
		}
	}
	if resp.Status == http.StatusAccepted && operationKind != "async" {
		return Result{Err: newErr(http.StatusBadGateway, "INCUS_OPERATION_MALFORMED", "incus lifecycle operation reference is malformed")}
	}

	if operationKind == "async" {
		if operationID == "" {
			return Result{Err: newErr(http.StatusBadGateway, "INCUS_OPERATION_MALFORMED", "incus lifecycle operation reference is malformed")}
		}
		waited := s.waitForOperation(ctx, operationID)
		if waited.Err != nil {
			return waited
		}
	}
	if action == ActionCreate {
		verified := s.verifyCreatedInstance(ctx, instance)
		if verified.Err != nil {
			return verified
		}
	}

	body, err := json.Marshal(Response{
		Action:        action,
		Instance:      instance,
		Status:        lifecycleSuccessStatus(operationKind),
		OperationID:   operationID,
		OperationKind: operationKind,
	})
	if err != nil {
		return Result{Err: newErr(http.StatusInternalServerError, "NORMALIZE_FAILED", "normalize lifecycle response")}
	}
	return Result{Status: http.StatusOK, Body: body}
}

func lifecycleSuccessStatus(operationKind string) string {
	if operationKind == "async" {
		return "operation-completed"
	}
	return "sync-ok"
}

func (s *Service) waitForOperation(ctx context.Context, operationID string) Result {
	if err := ctx.Err(); err != nil {
		return Result{Err: newErr(http.StatusServiceUnavailable, "REQUEST_CANCELLED", "request cancelled")}
	}
	resp := s.incus.Execute(ctx, &incus.ProxyRequest{
		Method: http.MethodGet,
		Path:   "/1.0/operations/" + url.PathEscape(operationID) + "/wait",
	})
	if resp == nil {
		return Result{Err: newErr(http.StatusServiceUnavailable, "INCUS_UNAVAILABLE", "incus lifecycle backend unavailable")}
	}
	if resp.Status == http.StatusNotFound {
		return Result{Err: newErr(http.StatusBadGateway, "INCUS_OPERATION_MISSING", "incus lifecycle operation disappeared before completion")}
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return Result{Err: newErr(resp.Status, mapIncusErrorCode(resp.Status), mapIncusErrorMessage(resp.Status))}
	}
	var env incusEnvelope
	if len(resp.Body) == 0 || json.Unmarshal(resp.Body, &env) != nil {
		return Result{Err: newErr(http.StatusBadGateway, "INCUS_OPERATION_MALFORMED", "incus lifecycle operation response is malformed")}
	}
	if env.Metadata.StatusCode >= 200 && env.Metadata.StatusCode < 300 {
		return Result{}
	}
	if env.Metadata.StatusCode == 0 {
		return Result{Err: newErr(http.StatusBadGateway, "INCUS_OPERATION_MALFORMED", "incus lifecycle operation response is malformed")}
	}
	return Result{Err: newErr(http.StatusBadGateway, "INCUS_OPERATION_FAILED", "incus lifecycle operation failed")}
}

func (s *Service) verifyCreatedInstance(ctx context.Context, instance string) Result {
	if err := ctx.Err(); err != nil {
		return Result{Err: newErr(http.StatusServiceUnavailable, "REQUEST_CANCELLED", "request cancelled")}
	}
	if err := ValidateInstanceName(instance); err != nil {
		return Result{Err: err}
	}
	resp := s.incus.Execute(ctx, &incus.ProxyRequest{
		Method: http.MethodGet,
		Path:   "/1.0/instances/" + url.PathEscape(instance),
	})
	if resp == nil {
		return Result{Err: newErr(http.StatusServiceUnavailable, "INCUS_UNAVAILABLE", "incus lifecycle backend unavailable")}
	}
	if resp.Status == http.StatusNotFound {
		return Result{Err: newErr(http.StatusBadGateway, "INCUS_INSTANCE_MISSING", "incus lifecycle create completed but instance is missing")}
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return Result{Err: newErr(resp.Status, mapIncusErrorCode(resp.Status), mapIncusErrorMessage(resp.Status))}
	}
	return Result{}
}

// incusEnvelope holds only the documented Incus REST envelope fields the
// lifecycle protocol reads to normalize operation references and terminal
// operation status. Other fields are intentionally ignored so no upstream
// guessing or leakage occurs.
type incusEnvelope struct {
	Type      string `json:"type"`
	Operation string `json:"operation"`
	Metadata  struct {
		StatusCode int    `json:"status_code"`
		Status     string `json:"status"`
		Err        string `json:"err"`
	} `json:"metadata"`
}

type incusProfileEnvelope struct {
	Metadata struct {
		Devices map[string]map[string]string `json:"devices"`
	} `json:"metadata"`
}

type profileRootDiskDevice struct {
	Name   string
	Device map[string]string
}

func selectProfileRootDisk(devices map[string]map[string]string) (profileRootDiskDevice, *Error) {
	matches := make([]profileRootDiskDevice, 0, 1)
	for name, device := range devices {
		if device["type"] != "disk" || device["path"] != "/" || device["source"] != "" {
			continue
		}
		copied := make(map[string]string, len(device)+1)
		for k, v := range device {
			copied[k] = v
		}
		matches = append(matches, profileRootDiskDevice{Name: name, Device: copied})
	}
	if len(matches) == 0 {
		return profileRootDiskDevice{}, newErr(http.StatusBadGateway, "INCUS_PROFILE_ROOT_DISK_UNAVAILABLE", "incus default profile root disk unavailable")
	}
	if len(matches) > 1 {
		return profileRootDiskDevice{}, newErr(http.StatusBadGateway, "INCUS_PROFILE_ROOT_DISK_AMBIGUOUS", "incus default profile root disk is ambiguous")
	}
	root := matches[0]
	if root.Device["pool"] == "" {
		return profileRootDiskDevice{}, newErr(http.StatusBadGateway, "INCUS_PROFILE_ROOT_DISK_UNAVAILABLE", "incus default profile root disk unavailable")
	}
	return root, nil
}

func (d profileRootDiskDevice) deviceWithSize(size string) map[string]string {
	device := make(map[string]string, len(d.Device)+1)
	for k, v := range d.Device {
		device[k] = v
	}
	device["size"] = size
	return device
}

// normalizeOperationID extracts the trailing path segment of an Incus
// operation URL (e.g. "/1.0/operations/<uuid>" -> "<uuid>"). It returns the
// cleaned id, or the raw value trimmed of whitespace if it is not a path.
// It never returns a value containing path traversal or separators.
func normalizeOperationID(op string) string {
	op = strings.TrimSpace(op)
	if op == "" {
		return ""
	}
	if i := strings.LastIndexByte(op, '/'); i >= 0 {
		op = op[i+1:]
	}
	op = strings.TrimSpace(op)
	if len(op) > 128 {
		return ""
	}
	return op
}

func mapIncusErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "INCUS_BAD_REQUEST"
	case http.StatusNotFound:
		return "INSTANCE_NOT_FOUND"
	case http.StatusConflict:
		return "INSTANCE_STATE_CONFLICT"
	case http.StatusServiceUnavailable:
		return "INCUS_UNAVAILABLE"
	default:
		return "INCUS_LIFECYCLE_FAILED"
	}
}

func mapIncusErrorMessage(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "incus rejected lifecycle request"
	case http.StatusNotFound:
		return "instance not found"
	case http.StatusConflict:
		return "instance state conflict"
	case http.StatusServiceUnavailable:
		return "incus unavailable"
	default:
		return "incus lifecycle operation failed"
	}
}

// lifecycleRoute is a parsed lifecycle request path.
type lifecycleRoute struct {
	action Action
	name   string // empty for create / capabilities
}

const (
	pathCapabilities = "/agent/v1/lifecycle/capabilities"
	pathCreate       = "/agent/v1/lifecycle/instances/create"
	pathInstances    = "/agent/v1/lifecycle/instances/"
)

// parseLifecyclePath maps an incoming agent path (other than the capabilities
// path, which is handled by the proxy directly) to a lifecycle route. It
// rejects path traversal and unsupported operations such as snapshot, exec,
// console, file, and migration by returning ok=false.
func parseLifecyclePath(path string) (lifecycleRoute, bool) {
	if path == pathCreate {
		return lifecycleRoute{action: ActionCreate}, true
	}
	if !strings.HasPrefix(path, pathInstances) {
		return lifecycleRoute{}, false
	}
	rest := path[len(pathInstances):]
	if rest == "" || strings.Contains(rest, "..") {
		return lifecycleRoute{}, false
	}
	// Supported shape: instances/{name}/start|stop|restart|delete with exactly
	// one slash separating the URL-encoded name and the action. Any extra
	// segment (e.g. /snapshot, /exec, /console, /files, /state) is rejected.
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return lifecycleRoute{}, false
	}
	if strings.Contains(parts[1], "/") {
		return lifecycleRoute{}, false
	}
	name, err := url.PathUnescape(parts[0])
	if err != nil {
		return lifecycleRoute{}, false
	}
	action := actionFromString(parts[1])
	if action == "" {
		return lifecycleRoute{}, false
	}
	return lifecycleRoute{action: action, name: name}, true
}

func actionFromString(s string) Action {
	switch s {
	case "start":
		return ActionStart
	case "stop":
		return ActionStop
	case "restart":
		return ActionRestart
	case "delete":
		return ActionDelete
	default:
		return ""
	}
}

// decodeStrict decodes raw JSON into dst with DisallowUnknownFields and a
// single-object guard. Decoder errors are classified to a safe message so
// attacker-controlled field names or value fragments never leak.
func decodeStrict(raw json.RawMessage, dst interface{}) *Error {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > maxBodyBytes {
		return newErr(http.StatusBadRequest, "BODY_TOO_LARGE", "request body too large")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		message := "request body is not valid JSON"
		if strings.Contains(err.Error(), "unknown field") {
			message = "request body contains an unknown field"
		}
		return newErr(http.StatusBadRequest, "INVALID_BODY", message)
	}
	if dec.More() {
		return newErr(http.StatusBadRequest, "INVALID_BODY", "request body must be a single object")
	}
	return nil
}

func decodeCreate(raw json.RawMessage) (CreateInstanceRequest, *Error) {
	if len(raw) == 0 {
		return CreateInstanceRequest{}, newErr(http.StatusBadRequest, "MISSING_BODY", "missing request body")
	}
	if len(raw) > maxBodyBytes {
		return CreateInstanceRequest{}, newErr(http.StatusBadRequest, "BODY_TOO_LARGE", "request body too large")
	}
	var req CreateInstanceRequest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		message := "request body is not valid JSON"
		if strings.Contains(err.Error(), "unknown field") {
			message = "request body contains an unknown field"
		}
		return CreateInstanceRequest{}, newErr(http.StatusBadRequest, "INVALID_BODY", message)
	}
	if dec.More() {
		return CreateInstanceRequest{}, newErr(http.StatusBadRequest, "INVALID_BODY", "request body must be a single object")
	}
	if err := ValidateCreate(req); err != nil {
		return CreateInstanceRequest{}, err
	}
	return req, nil
}

// ValidateCreate validates a create request's fields. Error messages identify
// the field and validation class only and never echo submitted values.
func ValidateCreate(req CreateInstanceRequest) *Error {
	if err := ValidateInstanceName(req.Name); err != nil {
		return err
	}
	if err := ValidateImage(req.Image); err != nil {
		return err
	}
	if req.CPUCount < 1 || req.CPUCount > maxCPUCount {
		return newErr(http.StatusBadRequest, "INVALID_LIMITS", "cpuCount out of range")
	}
	if req.MemoryBytes < 1 || req.MemoryBytes > maxMemoryBytes {
		return newErr(http.StatusBadRequest, "INVALID_LIMITS", "memoryBytes out of range")
	}
	if req.RootDiskBytes < 1 || req.RootDiskBytes > maxRootDiskBytes {
		return newErr(http.StatusBadRequest, "INVALID_LIMITS", "rootDiskBytes out of range")
	}
	return nil
}

var (
	instanceNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	imageRe        = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/+-]{0,127}$`)
)

// ValidateInstanceName enforces the Incus/DNS-label-safe allowlist. It rejects
// empty names, path traversal (..), slashes, shell metacharacters, and names
// requiring URL encoding. Hyphens are allowed but a name may not start with a
// hyphen.
func ValidateInstanceName(name string) *Error {
	if name == "" {
		return newErr(http.StatusBadRequest, "INVALID_INSTANCE_NAME", "instance name is required")
	}
	if len(name) > maxInstanceLen {
		return newErr(http.StatusBadRequest, "INVALID_INSTANCE_NAME", "instance name too long")
	}
	if strings.Contains(name, "..") {
		return newErr(http.StatusBadRequest, "INVALID_INSTANCE_NAME", "instance name must not contain path traversal")
	}
	if !instanceNameRe.MatchString(name) {
		return newErr(http.StatusBadRequest, "INVALID_INSTANCE_NAME", "instance name is not allowed")
	}
	return nil
}

// ValidateImage enforces a bounded image-alias allowlist. It rejects
// whitespace, shell metacharacters, path traversal, and values requiring
// shell quoting.
func ValidateImage(image string) *Error {
	if image == "" {
		return newErr(http.StatusBadRequest, "INVALID_IMAGE", "image is required")
	}
	if len(image) > maxImageLen {
		return newErr(http.StatusBadRequest, "INVALID_IMAGE", "image too long")
	}
	if strings.Contains(image, "..") {
		return newErr(http.StatusBadRequest, "INVALID_IMAGE", "image must not contain path traversal")
	}
	if !imageRe.MatchString(image) {
		return newErr(http.StatusBadRequest, "INVALID_IMAGE", "image is not allowed")
	}
	return nil
}

// formatSize returns a decimal byte count as a string for Incus disk/memory
// limits. Incus accepts raw byte integers as strings for limits.memory and
// root disk size.
func formatSize(n int64) string { return fmt.Sprintf("%d", n) }
