package proxy

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/anvil/proxy/internal/incus"
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
	if req.Path != "/1.0" && !strings.HasPrefix(req.Path, "/1.0/") {
		return fmt.Errorf("path outside incus api")
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
