package credentials

import (
	"errors"
	"testing"
)

func TestFromVCAPFindsWorkspaceBindingWithoutExposingOtherCredentials(t *testing.T) {
	got, err := FromVCAP(`{"user-provided":[{"name":"dgx-model","credentials":{"token":"private"}},{"name":"garage-instance","binding_name":"workspace-sync","instance_guid":"binding-guid","credentials":{"bucket":"sandbox-abc","region":"region-1","endpoint":"https://s3.example","access_key_id":"key","secret_access_key":"secret","session_token":"session"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "garage-instance" || got.BindingName != "workspace-sync" || got.InstanceID != "binding-guid" || got.Credentials.Bucket != "sandbox-abc" || got.Credentials.SessionToken != "session" {
		t.Fatalf("unexpected binding: %+v", got)
	}
}

func TestFromVCAPAcceptsNestedSchema(t *testing.T) {
	got, err := FromVCAP(`{"user-provided":[{"name":"workspace-sync","credentials":{"s3":{"bucket":"nested"}}}]}`)
	if err != nil || got.Credentials.Bucket != "nested" {
		t.Fatalf("binding=%+v err=%v", got, err)
	}
}

func TestFromVCAPNotFound(t *testing.T) {
	_, err := FromVCAP(`{"user-provided":[]}`)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error=%v", err)
	}
}
