package diagnostics

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
)

func clusterTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_clusters_list",
			Title: "Clusters: list",
			Description: "List the control planes this server can talk to. Every other tool takes a " +
				"'cluster' argument naming one of these; omitting it uses the default. Call this first when " +
				"asked about a control plane other than the current one, or when a cluster name is rejected, " +
				"rather than guessing a name.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"search": api.StringProp("Case insensitive substring to match against the cluster name. " +
					"Useful when a kubeconfig holds hundreds of contexts."),
				"limit": api.IntProp("Maximum number of clusters to return. Defaults to 50."),
			}),
			Handler: clustersList,
		},
	}
}

func clustersList(p api.Params) (*api.Result, error) {
	search := strings.ToLower(p.Args.OptionalString("search", ""))
	limit := p.Args.OptionalInt("limit", 50)
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	all := p.Provider.Targets()
	matched := all
	if search != "" {
		matched = matched[:0]
		for _, target := range all {
			if strings.Contains(strings.ToLower(target.Name), search) {
				matched = append(matched, target)
			}
		}
	}

	truncated := false
	shown := matched
	if limit > 0 && len(shown) > limit {
		shown, truncated = shown[:limit], true
	}

	payload := map[string]any{
		"count":     len(matched),
		"total":     len(all),
		"default":   p.Provider.Default(),
		"clusters":  shown,
		"truncated": truncated,
	}

	if len(matched) == 0 {
		return api.Structured(fmt.Sprintf(
			"No cluster name matched %q. This server knows about %d cluster(s).", search, len(all)), payload), nil
	}

	rows := make([][]string, 0, len(shown))
	for _, target := range shown {
		marker := ""
		if target.Default {
			marker = "*"
		}
		rows = append(rows, []string{
			marker,
			target.Name,
			string(target.Source),
			orDash(target.AuthProvider),
			orDash(target.Server),
		})
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%d cluster(s), default marked with *.\n", len(matched))
	if truncated {
		fmt.Fprintf(&text, "Showing the first %d, narrow the list with 'search'.\n", limit)
	}
	text.WriteString(api.Table([]string{"", "NAME", "SOURCE", "AUTH", "SERVER"}, rows))

	return api.Structured(text.String(), payload), nil
}
