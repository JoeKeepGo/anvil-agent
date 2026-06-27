package proxy

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/JoeKeepGo/anvil-agent/internal/incus"
)

func validateProxyRequest(req incus.ProxyRequest) error {
	if req.ID == "" {
		return fmt.Errorf("missing id")
	}
	if req.Method == "" {
		return fmt.Errorf("missing method")
	}
	if !isAllowedMethod(req.Method) {
		return fmt.Errorf("unsupported method")
	}
	if req.Path == "" {
		return fmt.Errorf("missing path")
	}
	if req.Path == "/agent/v1/state" || strings.HasPrefix(req.Path, "/agent/v1/") {
		return nil
	}
	if req.Path != "/1.0" && !strings.HasPrefix(req.Path, "/1.0/") {
		return fmt.Errorf("path outside incus api")
	}
	if req.Method != http.MethodGet {
		return fmt.Errorf("incus write method not allowed")
	}
	if !isAllowedIncusReadPath(req.Path) {
		return fmt.Errorf("incus read path not allowed")
	}
	return nil
}

func isAllowedMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isAllowedIncusReadPath(rawPath string) bool {
	parsed, err := url.ParseRequestURI(rawPath)
	if err != nil {
		return false
	}

	escapedPath := parsed.EscapedPath()
	switch escapedPath {
	case "/1.0", "/1.0/instances":
		return parsed.RawQuery == ""
	case "/1.0/images":
		return parsed.RawQuery == "" || isRecursionOneQuery(parsed.Query())
	case "/1.0/operations":
		return parsed.RawQuery == ""
	}

	if parsed.RawQuery != "" {
		return false
	}

	return hasAllowedDetailReadShape(escapedPath)
}

func isRecursionOneQuery(values url.Values) bool {
	return len(values) == 1 && len(values["recursion"]) == 1 && values.Get("recursion") == "1"
}

func hasAllowedDetailReadShape(escapedPath string) bool {
	segments := strings.Split(escapedPath, "/")
	if len(segments) != 4 || segments[0] != "" || segments[1] != "1.0" {
		return false
	}

	switch segments[2] {
	case "instances", "images", "operations":
	default:
		return false
	}

	segment := segments[3]
	if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, "/") {
		return false
	}

	decodedSegment, err := url.PathUnescape(segment)
	if err != nil {
		return false
	}
	return decodedSegment != "" &&
		decodedSegment != "." &&
		decodedSegment != ".." &&
		!strings.Contains(decodedSegment, "/")
}
