// Command diff emits a markdown changelog of message-set differences between two
// generated OCPP versions (e.g. v201 -> v21).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/shiv3/gocpp/internal/codegen/diff"
	"github.com/shiv3/gocpp/internal/codegen/loader"
)

func main() {
	from := flag.String("from", "v201", "from version package (v16, v201, v21)")
	to := flag.String("to", "v21", "to version package")
	flag.Parse()

	oldSet, err := loadFields(*from)
	if err != nil {
		fmt.Fprintln(os.Stderr, "diff:", err)
		os.Exit(1)
	}
	newSet, err := loadFields(*to)
	if err != nil {
		fmt.Fprintln(os.Stderr, "diff:", err)
		os.Exit(1)
	}
	d := diff.Compute(oldSet, newSet)
	fmt.Print(d.Markdown(versionString(*from), versionString(*to)))
}

// loadFields maps each action to its request's top-level property names.
func loadFields(version string) (map[string][]string, error) {
	ps, err := loader.LoadProfile(filepath.Join("internal", "codegen", "profiles", version+".yaml"))
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, prof := range ps.Profiles {
		for _, m := range prof.Messages {
			schema, err := loader.LoadSchema(filepath.Join("schemas", version, m.Request))
			if err != nil {
				return nil, err
			}
			props, _ := schema["properties"].(map[string]any)
			fields := make([]string, 0, len(props))
			for k := range props {
				fields = append(fields, k)
			}
			sort.Strings(fields)
			out[m.Name] = fields
		}
	}
	return out, nil
}

func versionString(version string) string {
	switch version {
	case "v16":
		return "1.6"
	case "v201":
		return "2.0.1"
	case "v21":
		return "2.1"
	default:
		return version
	}
}
