package credentials

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type S3 struct {
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
	Prefix          string `json:"prefix"`
}

type Binding struct {
	Name        string
	BindingName string
	InstanceID  string
	Credentials S3
}

var ErrNotFound = errors.New("workspace-sync service binding not found")

func FromVCAP(raw string) (Binding, error) {
	if strings.TrimSpace(raw) == "" {
		return Binding{}, ErrNotFound
	}
	var services map[string][]struct {
		Name        string          `json:"name"`
		BindingName string          `json:"binding_name"`
		InstanceID  string          `json:"instance_guid"`
		Credentials json.RawMessage `json:"credentials"`
	}
	if err := json.Unmarshal([]byte(raw), &services); err != nil {
		return Binding{}, fmt.Errorf("parse VCAP_SERVICES: %w", err)
	}
	for _, bindings := range services {
		for _, item := range bindings {
			if item.BindingName != "workspace-sync" && item.Name != "workspace-sync" && !strings.HasPrefix(item.Name, "workspace-sync-") {
				continue
			}
			var direct S3
			if err := json.Unmarshal(item.Credentials, &direct); err != nil {
				return Binding{}, fmt.Errorf("decode workspace-sync credentials: %w", err)
			}
			if direct.Bucket != "" {
				return Binding{Name: item.Name, BindingName: item.BindingName, InstanceID: item.InstanceID, Credentials: direct}, nil
			}
			var wrapped struct {
				S3 S3 `json:"s3"`
			}
			if err := json.Unmarshal(item.Credentials, &wrapped); err != nil {
				return Binding{}, fmt.Errorf("decode nested workspace-sync credentials: %w", err)
			}
			if wrapped.S3.Bucket != "" {
				return Binding{Name: item.Name, BindingName: item.BindingName, InstanceID: item.InstanceID, Credentials: wrapped.S3}, nil
			}
		}
	}
	return Binding{}, ErrNotFound
}
