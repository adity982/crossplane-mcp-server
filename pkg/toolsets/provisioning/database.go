package provisioning

import (
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/api"
)

const (
	enginePostgres = "postgresql"
	engineMySQL    = "mysql"
	engineMariaDB  = "mariadb"
	engineRedis    = "redis"
)

func databaseTools() []api.Tool {
	return []api.Tool{
		{
			Name:  "crossplane_database_create",
			Title: "Database: create",
			Description: "Create a database by asking this control plane's own platform API for one. " +
				"Finds the claim or composite kind that offers databases, fills in the fields its XRD declares " +
				"and applies it; Crossplane then provisions whatever the Composition says a database is, which " +
				"may be a cloud instance or, on a demo control plane, a fake one. " +
				"Pass 'dryRun' to see the manifest without creating anything, or 'kind' when the control plane " +
				"offers more than one database API.",
			InputSchema: api.Object(map[string]*jsonschema.Schema{
				"name": api.StringProp("Name for the database, for example 'orders-db'. " +
					"Must be a valid Kubernetes name: lower case letters, digits and dashes."),
				"namespace": namespaceProp,
				"engine": api.EnumProp("Database engine to ask for. Only set if the platform API offers a "+
					"choice; it is ignored when the API is engine-specific.",
					enginePostgres, engineMySQL, engineMariaDB, engineRedis),
				"version":   api.StringProp("Engine version, for example '16'."),
				"size":      api.EnumProp("How big the instance should be, in the sizes platform APIs usually offer.", sizeSmall, sizeMedium, sizeLarge),
				"storageGB": api.IntProp("Storage to request, in gigabytes."),
				"kind": api.StringProp("Kind to create, for example 'PostgreSQLInstance'. " +
					"Omit to let the server find the database API this control plane offers."),
				"apiVersion": api.StringProp("API version of the kind, for example 'platform.example.org/v1alpha1'. " +
					"Only needed when the same kind exists in more than one group."),
				"parameters": api.ObjectProp("Extra fields to set, for the parts of the platform API this tool " +
					"does not know about. Call crossplane_xrd_schema to see what the API accepts."),
				"dryRun": api.DryRunProp,
			}, "name"),
			Write:   true,
			Handler: databaseCreate,
		},
	}
}

func databaseCreate(p api.Params) (*api.Result, error) {
	req := request{
		noun:       "database",
		name:       p.Args.String("name"),
		namespace:  p.Args.OptionalString("namespace", ""),
		kind:       p.Args.OptionalString("kind", ""),
		apiVersion: p.Args.OptionalString("apiVersion", ""),
		parameters: p.Args.OptionalMap("parameters"),
		dryRun:     p.Args.OptionalBool("dryRun", false),
	}
	engine := p.Args.OptionalEnum("engine", enginePostgres, enginePostgres, engineMySQL, engineMariaDB, engineRedis)
	size := p.Args.OptionalEnum("size", sizeSmall, sizeSmall, sizeMedium, sizeLarge)
	version := p.Args.OptionalString("version", "")
	storage := p.Args.OptionalInt("storageGB", 0)
	if err := p.Args.Err(); err != nil {
		return api.Error(err), nil
	}

	req.hints = databaseHints(engine)
	req.values = []field{
		{names: []string{"engine", "engineType", "databaseEngine"}, value: engine},
		{names: []string{"version", "engineVersion", "databaseVersion"}, value: optional(version)},
		{names: []string{"size", "instanceSize", "plan", "class", "tier"}, value: size},
		{names: []string{"storageGB", "storageGb", "storage", "storageSize", "diskSize"}, value: optionalInt(storage)},
	}
	return provision(p, req)
}

// databaseHints orders the words a database API is likely to be named after,
// most specific first. "postgres" has to beat "database" so that a control
// plane offering both a PostgreSQLInstance and a generic Database ends up
// picking the right one.
func databaseHints(engine string) []string {
	specific := map[string][]string{
		enginePostgres: {"postgresql", "postgres", "psql", "pg"},
		engineMySQL:    {"mysql"},
		engineMariaDB:  {"mariadb", "maria", "mysql"},
		engineRedis:    {"redis", "cache", "keyvalue"},
	}
	return append(specific[engine], "database", "sql", "db", "datastore", "store")
}

// optional turns an omitted string argument into an absent field rather than
// an empty one. Writing "" into a platform API is not the same as leaving it
// to the Composition's default.
func optional(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func optionalInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}
