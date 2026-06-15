// Command codegen generates OCPP message types and profile vars from JSON
// schemas and a profile YAML.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

	structByName := map[string]ir.Struct{}
	enumByName := map[string]ir.Enum{}
	messageFiles := map[string]ir.File{}
	enumsFile := ir.File{Version: cfg.version, Package: "messages"}
	var profiles []profMsgs
	var allMessages []ir.Message

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
			reqStructs, reqEnums, err := loadTree(cfg.schemaDir, m.Request, reqStruct)
			if err != nil {
				return err
			}
			respStructs, respEnums, err := loadTree(cfg.schemaDir, m.Response, respStruct)
			if err != nil {
				return err
			}

			allStructs := slices.Concat(reqStructs, respStructs)
			msgStructs, err := filterNewStructs(allStructs, structByName)
			if err != nil {
				return err
			}
			for _, e := range append(reqEnums, respEnums...) {
				if err := addEnum(&enumsFile, enumByName, e); err != nil {
					return err
				}
			}
			if err := copySchema(cfg.schemaDir, m.Request, cfg.outRoot, cfg.version); err != nil {
				return err
			}
			if err := copySchema(cfg.schemaDir, m.Response, cfg.outRoot, cfg.version); err != nil {
				return err
			}

			messageFiles[snakeName(m.Name)+".go"] = ir.File{
				Version: cfg.version,
				Package: "messages",
				Structs: msgStructs,
			}
			msg := ir.Message{
				Action:         m.Name,
				Direction:      m.Dir,
				Request:        reqStruct,
				Response:       respStruct,
				RequestSchema:  m.Request,
				ResponseSchema: m.Response,
			}
			pm.msgs = append(pm.msgs, msg)
			allMessages = append(allMessages, msg)
		}
		profiles = append(profiles, pm)
	}

	for name, f := range messageFiles {
		needTime, needDecimal := scanNeeds(f.Structs)
		msgSrc, err := render.MessageFile("messages", f.Structs, nil, needTime, needDecimal)
		if err != nil {
			return fmt.Errorf("render message %s: %w", name, err)
		}
		if err := writeFile(filepath.Join(cfg.outRoot, cfg.version, "messages", name), msgSrc); err != nil {
			return err
		}
	}
	if len(enumsFile.Enums) > 0 {
		enumSrc, err := render.EnumsFile("messages", enumsFile.Enums)
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
		fname := snakeName(pm.name) + ".go"
		if err := writeFile(filepath.Join(cfg.outRoot, cfg.version, "profiles", fname), src); err != nil {
			return err
		}
	}
	registerSrc, err := render.RegisterFile(cfg.version, allMessages)
	if err != nil {
		return fmt.Errorf("render schema registration: %w", err)
	}
	if err := writeFile(filepath.Join(cfg.outRoot, cfg.version, "profiles", "register.go"), registerSrc); err != nil {
		return err
	}
	if err := writeEmbed(cfg.outRoot, cfg.version); err != nil {
		return err
	}
	return nil
}

func loadTree(schemaDir, schemaFile, goName string) ([]ir.Struct, []ir.Enum, error) {
	schema, err := loader.LoadSchema(filepath.Join(schemaDir, schemaFile))
	if err != nil {
		return nil, nil, err
	}
	structs, enums, err := ir.BuildStructTree(goName, schema)
	if err != nil {
		return nil, nil, err
	}
	renameGeneratedEnums(structs, enums)
	return structs, enums, nil
}

func filterNewStructs(structs []ir.Struct, seen map[string]ir.Struct) ([]ir.Struct, error) {
	out := make([]ir.Struct, 0, len(structs))
	for _, s := range structs {
		if existing, ok := seen[s.GoName]; ok {
			if !reflect.DeepEqual(existing, s) {
				return nil, fmt.Errorf("struct %s generated with conflicting fields", s.GoName)
			}
			continue
		}
		seen[s.GoName] = s
		out = append(out, s)
	}
	return out, nil
}

func addEnum(file *ir.File, seen map[string]ir.Enum, enum ir.Enum) error {
	if existing, ok := seen[enum.GoName]; ok {
		merged := mergeEnumValues(existing.Values, enum.Values)
		if !reflect.DeepEqual(existing.Values, merged) {
			existing.Values = merged
			seen[enum.GoName] = existing
			for i := range file.Enums {
				if file.Enums[i].GoName == enum.GoName {
					file.Enums[i] = existing
					break
				}
			}
		}
		return nil
	}
	seen[enum.GoName] = enum
	file.Enums = append(file.Enums, enum)
	return nil
}

func mergeEnumValues(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, v := range append(a, b...) {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func renameGeneratedEnums(structs []ir.Struct, enums []ir.Enum) {
	enumNames := map[string]string{}
	for si := range structs {
		if structs[si].GoName != "BootNotificationResponse" {
			continue
		}
		for fi := range structs[si].Fields {
			field := &structs[si].Fields[fi]
			if field.JSONName == "status" && field.Type == ir.TypeEnumRef {
				enumNames[field.EnumName] = "RegistrationStatus"
				field.EnumName = "RegistrationStatus"
			}
		}
	}
	for i := range enums {
		if replacement, ok := enumNames[enums[i].GoName]; ok {
			enums[i].GoName = "RegistrationStatus"
			_ = replacement
		}
	}
}

func scanNeeds(structs []ir.Struct) (bool, bool) {
	var needTime, needDecimal bool
	for _, s := range structs {
		for _, f := range s.Fields {
			if f.Type == ir.TypeDateTime || f.ElemType == ir.TypeDateTime {
				needTime = true
			}
			if f.Type == ir.TypeNumber || f.ElemType == ir.TypeNumber {
				needDecimal = true
			}
		}
	}
	return needTime, needDecimal
}

func copySchema(schemaDir, schemaFile, outRoot, version string) error {
	content, err := os.ReadFile(filepath.Join(schemaDir, schemaFile))
	if err != nil {
		return fmt.Errorf("copy schema %s: %w", schemaFile, err)
	}

	overridePath := filepath.Join("schemas", "overrides", version, schemaFile)
	overrideContent, err := os.ReadFile(overridePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return writeFile(filepath.Join(outRoot, version, "schemas", schemaFile), content)
		}
		return fmt.Errorf("read schema override %s: %w", overridePath, err)
	}

	var base any
	if err := json.Unmarshal(content, &base); err != nil {
		return fmt.Errorf("parse schema %s: %w", schemaFile, err)
	}
	var override any
	if err := json.Unmarshal(overrideContent, &override); err != nil {
		return fmt.Errorf("parse schema override %s: %w", overridePath, err)
	}
	merged, err := json.MarshalIndent(mergeJSON(base, override), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal merged schema %s: %w", schemaFile, err)
	}
	return writeFile(filepath.Join(outRoot, version, "schemas", schemaFile), merged)
}

func mergeJSON(base, override any) any {
	overrideObj, ok := override.(map[string]any)
	if !ok {
		return override
	}

	baseObj, _ := base.(map[string]any)
	merged := make(map[string]any, len(baseObj)+len(overrideObj))
	for k, v := range baseObj {
		merged[k] = v
	}
	for k, v := range overrideObj {
		if v == nil {
			delete(merged, k)
			continue
		}
		merged[k] = mergeJSON(merged[k], v)
	}
	return merged
}

func writeEmbed(outRoot, version string) error {
	content := []byte("// Code generated by gocpp codegen. DO NOT EDIT.\n\n" +
		"package schemas\n\n" +
		"import \"embed\"\n\n" +
		"//go:embed *.json\n" +
		"var FS embed.FS\n")
	return writeFile(filepath.Join(outRoot, version, "schemas", "embed.go"), content)
}

func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(content) {
		return nil
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
