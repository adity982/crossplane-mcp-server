package diagnostics

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
	"github.com/ravibagri5/crossplane-mcp-server/pkg/crossplane"
)

// unhealthyLimit caps how many failing resources a single call reports. If a
// control plane has more than this many broken resources the operator has a
// systemic problem, and a longer list would not help them find it.
const unhealthyLimit = 100

func diagnosticTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_status",
			Title: "Control plane: status",
			Description: "Give an overall picture of the Crossplane control plane: the Crossplane version, how " +
				"many providers, functions and configurations are installed and healthy, how many XRDs and " +
				"Compositions are defined, and how many managed, composite and claim resources exist along " +
				"with how many of them are Ready. Call this first when asked how a control plane is doing.",
			InputSchema: api.Object(nil),
			Handler:     status,
		},
		{
			Name:  "crossplane_unhealthy_resources",
			Title: "Control plane: what is failing",
			Description: "Find everything on the control plane that is not working: packages that are not " +
				"Installed or Healthy, XRDs that are not established, and managed, composite and claim " +
				"resources whose Ready or Synced condition is not True, each with the reason it is failing. " +
				"This is the tool to use for any question of the form 'what is broken?' or 'why is X failing?'.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"namespace": api.NamespaceProp,
				"scope": api.EnumProp("Limit the search to one part of the control plane. Defaults to 'all'.",
					"all", "packages", "managed", "composite", "claims"),
				"limit": api.IntProp("Maximum number of failing resources to report. Defaults to 100."),
			}),
			Handler: unhealthyResources,
		},
		{
			Name:  "crossplane_api_resources",
			Title: "Control plane: API surface",
			Description: "List the API kinds Crossplane has installed on the cluster, grouped by category " +
				"(managed, composite, claim). Use this to discover the exact kind and API group to pass to the " +
				"other tools, or to check whether a provider actually installed the kind you expect.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"category": api.EnumProp("Category to list. Defaults to 'all'.",
					"all", crossplane.CategoryManaged, crossplane.CategoryComposite, crossplane.CategoryClaim),
				"group":  api.GroupProp,
				"search": api.StringProp("Case insensitive substring to match against the kind or group name."),
			}),
			Handler: apiResources,
		},
	}
}

func status(p api.Params) (*api.Result, error) {
	var text strings.Builder
	payload := map[string]any{}

	namespace, version, err := p.Client.CrossplaneVersion(p)
	switch {
	case err != nil:
		text.WriteString("Crossplane core deployment: not found. " +
			"Either Crossplane is not installed on this cluster or the caller cannot list deployments.\n")
		payload["crossplaneInstalled"] = false
	default:
		fmt.Fprintf(&text, "Crossplane %s in namespace %s.\n", version, namespace)
		payload["crossplaneInstalled"] = true
		payload["crossplaneVersion"] = version
		payload["crossplaneNamespace"] = namespace
	}
	if kubeVersion, err := p.Client.ServerVersion(); err == nil {
		fmt.Fprintf(&text, "Kubernetes %s.\n", kubeVersion)
		payload["kubernetesVersion"] = kubeVersion
	}

	packageRows := make([][]string, 0, 3)
	packageStats := map[string]any{}
	for _, kind := range []crossplane.PackageKind{
		crossplane.PackageProvider, crossplane.PackageFunction, crossplane.PackageConfiguration,
	} {
		installed, err := p.Client.Packages(p, kind)
		if err != nil {
			packageRows = append(packageRows, []string{string(kind) + "s", "?", "?", err.Error()})
			continue
		}
		healthy := 0
		var unhealthy []string
		for _, pkg := range installed {
			if pkg.Installed == crossplane.StatusTrue && pkg.Healthy == crossplane.StatusTrue {
				healthy++
				continue
			}
			unhealthy = append(unhealthy, pkg.Name)
		}
		packageRows = append(packageRows, []string{
			string(kind) + "s", strconv.Itoa(len(installed)), strconv.Itoa(healthy), api.OrDash(strings.Join(unhealthy, ", ")),
		})
		packageStats[strings.ToLower(string(kind))+"s"] = map[string]any{
			"installed": len(installed),
			"healthy":   healthy,
			"unhealthy": unhealthy,
		}
	}
	payload["packages"] = packageStats
	api.Section(&text, "Packages:",
		api.Table([]string{"TYPE", "INSTALLED", "HEALTHY", "UNHEALTHY"}, packageRows))

	definitionRows := make([][]string, 0, 2)
	if xrds, err := p.Client.XRDs(p); err == nil {
		established := 0
		for _, xrd := range xrds {
			if xrd.Ready == crossplane.StatusTrue {
				established++
			}
		}
		definitionRows = append(definitionRows,
			[]string{"CompositeResourceDefinitions", strconv.Itoa(len(xrds)), strconv.Itoa(established)})
		payload["xrds"] = map[string]any{"total": len(xrds), "established": established}
	}
	if found, err := p.Client.Compositions(p, ""); err == nil {
		definitionRows = append(definitionRows, []string{"Compositions", strconv.Itoa(len(found)), "-"})
		payload["compositions"] = map[string]any{"total": len(found)}
	}
	api.Section(&text, "Platform APIs:", api.Table([]string{"KIND", "TOTAL", "ESTABLISHED"}, definitionRows))

	resourceRows := make([][]string, 0, 3)
	resourceStats := map[string]any{}
	for _, entry := range []struct {
		category string
		label    string
	}{
		{crossplane.CategoryManaged, "Managed resources"},
		{crossplane.CategoryComposite, "Composite resources"},
		{crossplane.CategoryClaim, "Claims"},
	} {
		result, err := p.Client.Query(p, crossplane.Query{Category: entry.category})
		if err != nil {
			resourceRows = append(resourceRows, []string{entry.label, "?", "?", err.Error()})
			continue
		}
		summaries := crossplane.Summaries(result.Objects, false)
		ready, synced := countConditions(summaries)
		resourceRows = append(resourceRows, []string{
			entry.label, strconv.Itoa(len(summaries)), strconv.Itoa(ready), strconv.Itoa(synced),
		})
		resourceStats[entry.category] = map[string]any{
			"total":    len(summaries),
			"ready":    ready,
			"notReady": len(summaries) - ready,
			"synced":   synced,
			"kinds":    len(result.Kinds),
		}
	}
	payload["resources"] = resourceStats
	api.Section(&text, "Resources:", api.Table([]string{"TYPE", "TOTAL", "READY", "SYNCED"}, resourceRows))

	return api.Structured(text.String(), payload), nil
}

func unhealthyResources(p api.Params) (*api.Result, error) {
	namespace := p.Args.OptionalString("namespace", "")
	scope := p.Args.OptionalEnum("scope", "all", "all", "packages", "managed", "composite", "claims")
	limit := p.Args.OptionalInt("limit", unhealthyLimit)
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	var (
		text    strings.Builder
		payload = map[string]any{}
		total   int
	)

	if scope == "all" || scope == "packages" {
		rows := make([][]string, 0, 8)
		broken := make([]map[string]any, 0, 8)
		for _, kind := range []crossplane.PackageKind{
			crossplane.PackageProvider, crossplane.PackageFunction, crossplane.PackageConfiguration,
		} {
			installed, err := p.Client.Packages(p, kind)
			if err != nil {
				continue
			}
			for _, pkg := range installed {
				if pkg.Installed == crossplane.StatusTrue && pkg.Healthy == crossplane.StatusTrue {
					continue
				}
				rows = append(rows, []string{
					string(kind), pkg.Name, pkg.Installed, pkg.Healthy, pkg.Age, api.OrDash(pkg.Message),
				})
				broken = append(broken, map[string]any{
					"type": string(kind), "name": pkg.Name, "package": pkg.Package,
					"installed": pkg.Installed, "healthy": pkg.Healthy, "message": pkg.Message,
				})
			}
		}
		total += len(broken)
		payload["packages"] = broken
		if len(rows) > 0 {
			api.Section(&text, fmt.Sprintf("Unhealthy packages (%d):", len(rows)),
				api.Table([]string{"TYPE", "NAME", "INSTALLED", "HEALTHY", "AGE", "MESSAGE"}, rows))
		}

		// An XRD that is not established means its composite and claim CRDs
		// were never created, which looks to a user like a missing kind
		// rather than a broken definition. It belongs in this report.
		if xrds, err := p.Client.XRDs(p); err == nil {
			rows := make([][]string, 0, 4)
			broken := make([]crossplane.CompositeResourceDefinition, 0, 4)
			for _, xrd := range xrds {
				if xrd.Healthy() {
					continue
				}
				broken = append(broken, xrd)
				rows = append(rows, []string{xrd.Name, xrd.CompositeKind, xrd.Ready, xrd.Age, api.OrDash(xrd.Message)})
			}
			total += len(broken)
			payload["xrds"] = broken
			if len(rows) > 0 {
				api.Section(&text, fmt.Sprintf("Unhealthy CompositeResourceDefinitions (%d):", len(rows)),
					api.Table([]string{"NAME", "COMPOSITE", "ESTABLISHED", "AGE", "MESSAGE"}, rows))
			}
		}
	}

	for _, entry := range []struct {
		scope    string
		category string
		label    string
	}{
		{"managed", crossplane.CategoryManaged, "managed resources"},
		{"composite", crossplane.CategoryComposite, "composite resources"},
		{"claims", crossplane.CategoryClaim, "claims"},
	} {
		if scope != "all" && scope != entry.scope {
			continue
		}
		result, err := p.Client.Query(p, crossplane.Query{Category: entry.category, Namespace: namespace})
		if err != nil {
			api.Section(&text, "Could not check "+entry.label+":", err.Error())
			continue
		}
		failing := crossplane.Summaries(result.Objects, true)
		total += len(failing)
		payload[entry.category] = failing
		if len(failing) == 0 {
			continue
		}

		shown := failing
		truncated := false
		if len(shown) > limit {
			shown, truncated = shown[:limit], true
		}
		rows := make([][]string, 0, len(shown))
		for _, s := range shown {
			rows = append(rows, []string{
				s.Kind, api.OrDash(s.Namespace), s.Name, s.Ready, s.Synced, s.Age, api.OrDash(s.Message),
			})
		}
		title := fmt.Sprintf("Failing %s (%d):", entry.label, len(failing))
		if truncated {
			title = fmt.Sprintf("Failing %s (%d, showing first %d):", entry.label, len(failing), limit)
		}
		api.Section(&text, title, api.Table(
			[]string{"KIND", "NAMESPACE", "NAME", "READY", "SYNCED", "AGE", "MESSAGE"}, rows))
	}

	payload["total"] = total
	if total == 0 {
		return api.Structured("Nothing is failing: every package, XRD and resource in scope reports healthy.",
			payload), nil
	}
	return api.Structured(fmt.Sprintf("%d item(s) are not healthy.\n\n%s", total, text.String()), payload), nil
}

func apiResources(p api.Params) (*api.Result, error) {
	category := p.Args.OptionalEnum("category", "all",
		"all", crossplane.CategoryManaged, crossplane.CategoryComposite, crossplane.CategoryClaim)
	group := p.Args.OptionalString("group", "")
	search := strings.ToLower(p.Args.OptionalString("search", ""))
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	all, err := p.Client.APIResources(p)
	if err != nil {
		return api.Error(err), nil
	}

	matched := make([]crossplane.APIResource, 0, 64)
	for _, r := range all {
		if !r.HasCategory(crossplane.CategoryCrossplane) && !isCrossplaneGroup(r.Group) {
			continue
		}
		if category != "all" && !r.HasCategory(category) {
			continue
		}
		if group != "" && !strings.EqualFold(r.Group, group) {
			continue
		}
		if search != "" &&
			!strings.Contains(strings.ToLower(r.Kind), search) &&
			!strings.Contains(strings.ToLower(r.Group), search) {
			continue
		}
		matched = append(matched, r)
	}

	if len(matched) == 0 {
		return api.Structured("No Crossplane API resources matched.",
			map[string]any{"count": 0, "resources": matched}), nil
	}

	rows := make([][]string, 0, len(matched))
	for _, r := range matched {
		scope := "Cluster"
		if r.Namespaced {
			scope = "Namespaced"
		}
		rows = append(rows, []string{r.Kind, r.APIVersion(), scope, strings.Join(r.Categories, ",")})
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%d Crossplane API resource(s).\n", len(matched))
	text.WriteString(api.Table([]string{"KIND", "APIVERSION", "SCOPE", "CATEGORIES"}, rows))

	return api.Structured(text.String(), map[string]any{"count": len(matched), "resources": matched}), nil
}

// isCrossplaneGroup recognises the Crossplane core API groups. Some of them,
// such as pkg.crossplane.io, are not tagged with the crossplane category.
func isCrossplaneGroup(group string) bool {
	return strings.HasSuffix(group, ".crossplane.io")
}

func countConditions(summaries []crossplane.Summary) (ready, synced int) {
	for _, s := range summaries {
		if s.Ready == crossplane.StatusTrue {
			ready++
		}
		if s.Synced == crossplane.StatusTrue {
			synced++
		}
	}
	return ready, synced
}
