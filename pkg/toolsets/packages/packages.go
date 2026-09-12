package packages

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

func packageTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_providers_list",
			Title: "Providers: list",
			Description: "List the installed Crossplane providers with the package they were installed from, " +
				"their Installed and Healthy conditions and their active revision. Providers are what add " +
				"managed resource kinds to the control plane, so this is usually the first thing to check when " +
				"a managed resource kind is missing.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"unhealthyOnly": api.BoolProp("Return only providers that are not both Installed and Healthy."),
			}),
			Handler: listPackages(crossplane.PackageProvider),
		},
		{
			Name:  "crossplane_functions_list",
			Title: "Functions: list",
			Description: "List the installed composition functions with their Installed and Healthy conditions. " +
				"Functions run the pipeline of a Composition, so a function that is not Healthy will stop every " +
				"Composition that references it from producing resources.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"unhealthyOnly": api.BoolProp("Return only functions that are not both Installed and Healthy."),
			}),
			Handler: listPackages(crossplane.PackageFunction),
		},
		{
			Name:  "crossplane_configurations_list",
			Title: "Configurations: list",
			Description: "List the installed Crossplane configurations with their Installed and Healthy " +
				"conditions. A configuration packages XRDs and Compositions, so this shows which platform APIs " +
				"were installed from a package rather than applied directly.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"unhealthyOnly": api.BoolProp("Return only configurations that are not both Installed and Healthy."),
			}),
			Handler: listPackages(crossplane.PackageConfiguration),
		},
		{
			Name:  "crossplane_package_get",
			Title: "Package: describe",
			Description: "Describe a single provider, function or configuration together with all of its " +
				"revisions. Package revisions carry the detail a failing package hides: image pull errors, " +
				"unmet dependencies and RBAC permission requests all surface on the revision.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"type": api.EnumProp("Kind of package to describe.", "Provider", "Function", "Configuration"),
				"name": api.StringProp("Name of the package, for example 'provider-aws-s3'."),
			}, "type", "name"),
			Handler: packageGet,
		},
	}
}

// listPackages builds the handler shared by the three package list tools.
func listPackages(kind crossplane.PackageKind) api.Handler {
	return func(p api.Params) (*api.Result, error) {
		unhealthyOnly := p.Args.OptionalBool("unhealthyOnly", false)
		if err := p.Args.Err(); err != nil {
			return api.Error(err), nil
		}

		installed, err := p.Client.Packages(p, kind)
		if err != nil {
			return api.Error(err), nil
		}

		kept := make([]crossplane.Package, 0, len(installed))
		for _, pkg := range installed {
			if unhealthyOnly && pkg.Installed == crossplane.StatusTrue && pkg.Healthy == crossplane.StatusTrue {
				continue
			}
			kept = append(kept, pkg)
		}

		noun := strings.ToLower(string(kind)) + "s"
		if len(kept) == 0 {
			message := fmt.Sprintf("No %s installed.", noun)
			if unhealthyOnly {
				message = fmt.Sprintf("All %d installed %s are Installed and Healthy.", len(installed), noun)
			}
			return api.Structured(message, map[string]any{"count": 0, "packages": kept}), nil
		}

		rows := make([][]string, 0, len(kept))
		healthy := 0
		for _, pkg := range kept {
			if pkg.Installed == crossplane.StatusTrue && pkg.Healthy == crossplane.StatusTrue {
				healthy++
			}
			rows = append(rows, []string{
				pkg.Name, pkg.Package, pkg.Installed, pkg.Healthy, pkg.Age, orDash(pkg.Message),
			})
		}

		var text strings.Builder
		fmt.Fprintf(&text, "%d %s (%d healthy).\n", len(kept), noun, healthy)
		text.WriteString(api.Table([]string{"NAME", "PACKAGE", "INSTALLED", "HEALTHY", "AGE", "MESSAGE"}, rows))

		return api.Structured(text.String(), map[string]any{
			"count":    len(kept),
			"healthy":  healthy,
			"packages": kept,
		}), nil
	}
}

func packageGet(p api.Params) (*api.Result, error) {
	packageType := p.Args.OptionalEnum("type", "", "Provider", "Function", "Configuration")
	name := p.Args.String("name")
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}
	kind := crossplane.PackageKind(packageType)

	installed, err := p.Client.Packages(p, kind)
	if err != nil {
		return api.Error(err), nil
	}

	var found *crossplane.Package
	for i := range installed {
		if installed[i].Name == name {
			found = &installed[i]
			break
		}
	}
	if found == nil {
		return api.Errorf("%s %q is not installed on this control plane", kind, name), nil
	}

	revisions, revErr := p.Client.Revisions(p, kind, name)

	var text strings.Builder
	fmt.Fprintf(&text, "%s %s\npackage: %s\ninstalled: %s\nhealthy: %s\nage: %s",
		kind, found.Name, found.Package, found.Installed, found.Healthy, found.Age)
	if found.CurrentRevision != "" {
		fmt.Fprintf(&text, "\ncurrent revision: %s", found.CurrentRevision)
	}
	if found.RevisionActivationPolicy != "" {
		fmt.Fprintf(&text, "\nrevision activation policy: %s", found.RevisionActivationPolicy)
	}

	api.Section(&text, "Conditions:", renderConditions(found.Conditions))
	switch {
	case revErr != nil:
		api.Section(&text, "Revisions:", "could not be read: "+revErr.Error())
	default:
		api.Section(&text, "Revisions:", renderRevisions(revisions))
	}

	payload := map[string]any{
		"package":   found,
		"revisions": revisions,
	}
	return api.Structured(text.String(), payload), nil
}

func renderConditions(conditions []crossplane.Condition) string {
	if len(conditions) == 0 {
		return "none reported yet"
	}
	rows := make([][]string, 0, len(conditions))
	for _, c := range conditions {
		rows = append(rows, []string{c.Type, c.Status, orDash(c.Reason), orDash(c.Message)})
	}
	return api.Table([]string{"TYPE", "STATUS", "REASON", "MESSAGE"}, rows)
}

func renderRevisions(revisions []crossplane.Summary) string {
	if len(revisions) == 0 {
		return "none"
	}
	rows := make([][]string, 0, len(revisions))
	for _, r := range revisions {
		rows = append(rows, []string{r.Name, r.Synced, r.Ready, r.Age, orDash(r.Message)})
	}
	return api.Table([]string{"NAME", "INSTALLED", "HEALTHY", "AGE", "MESSAGE"}, rows)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
