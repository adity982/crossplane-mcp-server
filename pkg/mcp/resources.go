package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

const (
	xrdResourceScheme  = "crossplane"
	xrdResourceHost    = "xrd"
	xrdRefreshInterval = 30 * time.Second
)

func (s *Server) registerXRDResourceTemplate() {
	s.sdk.AddResourceTemplate(&sdk.ResourceTemplate{
		URITemplate: "crossplane://xrd/{group}/{kind}/{version}",
		Name:        "crossplane-xrd-schema",
		Title:       "Crossplane XRD schema",
		Description: "OpenAPI schema and example manifest for a Crossplane composite resource definition version.",
		MIMEType:    "application/json",
	}, s.readXRDResource)
}

func (s *Server) reconcileXRDResources(ctx context.Context) {
	ticker := time.NewTicker(xrdRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.refreshXRDResources(ctx); err != nil {
				s.config.Logger.Warn("cannot refresh XRD resources", "error", err)
			}
		}
	}
}

func (s *Server) refreshXRDResources(ctx context.Context) error {
	client, err := s.config.Provider.Client("")
	if err != nil {
		return err
	}
	schemas, err := client.XRDSchemas(ctx)
	if err != nil {
		return err
	}

	next := make(map[string]struct{}, len(schemas))
	s.resourcesMu.Lock()
	defer s.resourcesMu.Unlock()
	for _, schema := range schemas {
		uri := xrdResourceURI(schema.Group, schema.CompositeKind, schema.Version)
		next[uri] = struct{}{}
		if _, exists := s.xrdResources[uri]; exists {
			continue
		}
		s.sdk.AddResource(&sdk.Resource{
			URI:         uri,
			Name:        schema.Name + "/" + schema.Version,
			Title:       fmt.Sprintf("%s %s schema", schema.CompositeKind, schema.Version),
			Description: fmt.Sprintf("XRD schema for %s %s", schema.APIVersion, schema.CompositeKind),
			MIMEType:    "application/json",
		}, s.readXRDResource)
	}

	for uri := range s.xrdResources {
		if _, exists := next[uri]; !exists {
			s.sdk.RemoveResources(uri)
		}
	}
	s.xrdResources = next

	s.config.Logger.Debug("refreshed XRD resources", "resources", len(next), "cluster", client.Target())
	return nil
}

func (s *Server) readXRDResource(ctx context.Context, request *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
	group, kind, version, err := parseXRDResourceURI(request.Params.URI)
	if err != nil {
		return nil, sdk.ResourceNotFoundError(request.Params.URI)
	}
	client, err := s.config.Provider.Client("")
	if err != nil {
		return nil, err
	}
	schema, err := client.XRDSchemaForType(ctx, group, kind, version)
	if err != nil {
		var notFound *crossplane.ErrNotFound
		if errors.As(err, &notFound) {
			return nil, sdk.ResourceNotFoundError(request.Params.URI)
		}
		return nil, err
	}
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cannot encode XRD schema: %w", err)
	}
	return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{
		URI:      request.Params.URI,
		MIMEType: "application/json",
		Text:     string(data),
	}}}, nil
}

func xrdResourceURI(group, kind, version string) string {
	return (&url.URL{
		Scheme: xrdResourceScheme,
		Host:   xrdResourceHost,
		Path:   "/" + strings.Join([]string{group, kind, version}, "/"),
	}).String()
}

func parseXRDResourceURI(raw string) (group, kind, version string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != xrdResourceScheme || parsed.Host != xrdResourceHost {
		return "", "", "", fmt.Errorf("invalid XRD resource URI %q", raw)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("invalid XRD resource URI %q", raw)
	}
	return parts[0], parts[1], parts[2], nil
}
