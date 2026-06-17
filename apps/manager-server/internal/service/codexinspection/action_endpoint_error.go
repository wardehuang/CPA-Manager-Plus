package codexinspection

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type actionEndpointError struct {
	Endpoint string
	Err      error
}

func combineActionEndpointErrors(items ...actionEndpointError) error {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if item.Err == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %v", item.Endpoint, item.Err))
	}
	if len(parts) == 0 {
		return nil
	}
	return errors.New(strings.Join(parts, "; "))
}

func shouldFallbackManagement(status int) bool {
	return status == http.StatusNotFound || status == http.StatusMethodNotAllowed
}
