package publisher

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/overseer/overseer/pkg/nodestate"
)

const saTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
const saCACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

// CRDPublisher writes NodeState snapshots to the Kubernetes API server as
// overseer.io/v1alpha1 NodeState custom resources using server-side apply.
// NewCRD returns nil (no error) when the in-cluster service-account files are
// absent, so the caller can safely skip this publisher on a developer laptop.
type CRDPublisher struct {
	client *http.Client
	base   string
	token  string
}

// NewCRD returns a CRDPublisher wired to the in-cluster API server, or nil
// when KUBERNETES_SERVICE_HOST is unset (i.e. running outside a cluster).
func NewCRD() (*CRDPublisher, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" {
		return nil, nil
	}

	token, err := os.ReadFile(saTokenPath)
	if err != nil {
		return nil, fmt.Errorf("crd publisher: read token: %w", err)
	}
	caPEM, err := os.ReadFile(saCACertPath)
	if err != nil {
		return nil, fmt.Errorf("crd publisher: read ca cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("crd publisher: parse CA cert")
	}

	return &CRDPublisher{
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool},
			},
		},
		base:  fmt.Sprintf("https://%s:%s", host, port),
		token: string(token),
	}, nil
}

// crdObject is the Kubernetes API envelope for a NodeState custom resource.
type crdObject struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	Metadata   map[string]string   `json:"metadata"`
	Spec       nodestate.NodeState `json:"spec"`
}

// Publish applies the NodeState to the API server via server-side apply (SSA).
// A nil receiver is a no-op so callers need not nil-check.
func (c *CRDPublisher) Publish(ctx context.Context, ns nodestate.NodeState) error {
	if c == nil {
		return nil
	}
	obj := crdObject{
		APIVersion: "overseer.io/v1alpha1",
		Kind:       "NodeState",
		Metadata:   map[string]string{"name": ns.NodeName},
		Spec:       ns,
	}
	body, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("crd publisher: marshal: %w", err)
	}

	url := fmt.Sprintf(
		"%s/apis/overseer.io/v1alpha1/nodestates/%s?fieldManager=overseer-agent&force=true",
		c.base, ns.NodeName,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/apply-patch+yaml")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("crd publisher: apply patch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("crd publisher: server returned %d", resp.StatusCode)
	}
	return nil
}
