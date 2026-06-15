// Command codegen generates OCPP message types and profile vars from JSON
// schemas and a profile YAML.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/shiv3/gocpp/internal/codegen/ir"
	"github.com/shiv3/gocpp/internal/codegen/loader"
	"github.com/shiv3/gocpp/internal/codegen/render"
)

type genConfig struct {
	version     string
	profileYAML string
	schemaDir   string
	outRoot     string
}

func main() {
	version := flag.String("version", "v16", "OCPP version package (v16, v201, v21)")
	flag.Parse()

	cfg := genConfig{
		version:     *version,
		profileYAML: filepath.Join("internal", "codegen", "profiles", *version+".yaml"),
		schemaDir:   filepath.Join("schemas", *version),
		outRoot:     ".",
	}
	if err := generate(cfg); err != nil {
		log.Fatalf("codegen: %v", err)
	}
	fmt.Printf("codegen: generated %s\n", *version)
}

func generate(cfg genConfig) error {
	ps, err := loader.LoadProfile(cfg.profileYAML)
	if err != nil {
		return err
	}

	type profMsgs struct {
		name string
		msgs []ir.Message
	}

	structByName := map[string]bool{}
	enumByName := map[string]bool{}
	messageFiles := map[string]ir.File{}
	enumsFile := ir.File{Version: cfg.version, Package: "messages"}
	var profiles []profMsgs

	profileNames := make([]string, 0, len(ps.Profiles))
	for name := range ps.Profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)

	for _, profName := range profileNames {
		prof := ps.Profiles[profName]
		pm := profMsgs{name: profName}
		for _, m := range prof.Messages {
			reqStruct := m.Name + "Request"
			respStruct := m.Name + "Response"
			msgFile := ir.File{Version: cfg.version, Package: "messages"}
			if err := addStruct(&msgFile, cfg.schemaDir, m.Request, reqStruct, structByName, nil); err != nil {
				return err
			}
			if err := addStruct(&msgFile, cfg.schemaDir, m.Response, respStruct, structByName, enumByName); err != nil {
				return err
			}
			for _, e := range msgFile.Enums {
				enumsFile.Enums = append(enumsFile.Enums, e)
			}
			msgFile.Enums = nil
			messageFiles[snakeName(m.Name)+".go"] = msgFile
			pm.msgs = append(pm.msgs, ir.Message{
				Action:    m.Name,
				Direction: m.Dir,
				Request:   reqStruct,
				Response:  respStruct,
			})
		}
		profiles = append(profiles, pm)
	}

	for name, f := range messageFiles {
		msgSrc, err := render.Structs(f)
		if err != nil {
			return fmt.Errorf("render message %s: %w", name, err)
		}
		if err := writeFile(filepath.Join(cfg.outRoot, cfg.version, "messages", name), msgSrc); err != nil {
			return err
		}
	}
	if len(enumsFile.Enums) > 0 {
		enumSrc, err := render.Enums(enumsFile)
		if err != nil {
			return fmt.Errorf("render enums: %w", err)
		}
		if err := writeFile(filepath.Join(cfg.outRoot, cfg.version, "messages", "enums.go"), enumSrc); err != nil {
			return err
		}
	}

	for _, pm := range profiles {
		pf := ir.File{Version: cfg.version, Messages: pm.msgs}
		src, err := render.Profile(pf, pm.name)
		if err != nil {
			return fmt.Errorf("render profile %s: %w", pm.name, err)
		}
		fname := strings.ToLower(pm.name) + ".go"
		if err := writeFile(filepath.Join(cfg.outRoot, cfg.version, "profiles", fname), src); err != nil {
			return err
		}
	}
	return nil
}

func addStruct(file *ir.File, schemaDir, schemaFile, goName string, structSeen, enumSeen map[string]bool) error {
	if structSeen[goName] {
		return nil
	}
	schema, err := loader.LoadSchema(filepath.Join(schemaDir, schemaFile))
	if err != nil {
		return err
	}
	s, enums, err := ir.BuildStruct(goName, schema)
	if err != nil {
		return err
	}
	renameGeneratedEnums(&s, enums)
	file.Structs = append(file.Structs, s)
	structSeen[goName] = true
	for _, e := range enums {
		if enumSeen == nil || !enumSeen[e.GoName] {
			file.Enums = append(file.Enums, e)
			if enumSeen != nil {
				enumSeen[e.GoName] = true
			}
		}
	}
	return nil
}

func renameGeneratedEnums(s *ir.Struct, enums []ir.Enum) {
	enumNames := map[string]string{}
	for i := range enums {
		if s.GoName == "BootNotificationResponse" && enums[i].GoName == "Status" {
			enumNames[enums[i].GoName] = "RegistrationStatus"
			enums[i].GoName = "RegistrationStatus"
		}
	}
	for i := range s.Fields {
		if replacement, ok := enumNames[s.Fields[i].EnumName]; ok {
			s.Fields[i].EnumName = replacement
		}
	}
}

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func snakeName(name string) string {
	var b strings.Builder
	for i, r := range name {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
