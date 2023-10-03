// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this currentFile except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"
	apiextinternal "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	crdmarkers "sigs.k8s.io/controller-tools/pkg/crd/markers"
	"sigs.k8s.io/controller-tools/pkg/markers"
	"sigs.k8s.io/yaml"

	"istio.io/tools/pkg/protomodel"
)

// Some special types with predefined schemas.
// This is to catch cases where solo apis contain recursive definitions
// Normally these would result in stack-overflow errors when generating the open api schema
// The imperfect solution, is to just generate an empty object for these types
var specialSoloTypes = map[string]*apiext.JSONSchemaProps{
	"core.solo.io.Metadata": {
		Type: openapi3.TypeObject,
	},
	"google.protobuf.ListValue": {
		Items: &apiext.JSONSchemaPropsOrArray{Schema: &apiext.JSONSchemaProps{Type: "object"}},
	},
	"google.protobuf.Struct": {
		Type:                   openapi3.TypeObject,
		XPreserveUnknownFields: Ptr(true),
	},
	"google.protobuf.Any": {
		Type:                   openapi3.TypeObject,
		XPreserveUnknownFields: Ptr(true),
	},
	"google.protobuf.Value": {
		XPreserveUnknownFields: Ptr(true),
	},
	"google.protobuf.BoolValue": {
		Type:     "boolean",
		Nullable: true,
	},
	"google.protobuf.StringValue": {
		Type:     "string",
		Nullable: true,
	},
	"google.protobuf.DoubleValue": {
		Type:     "number",
		Nullable: true,
	},
	"google.protobuf.Int32Value": {
		Type:     "integer",
		Nullable: true,
		// Min: math.MinInt32,
		// Max: math.MaxInt32,
	},
	"google.protobuf.Int64Value": {
		Type:     "integer",
		Nullable: true,
		// Min: math.MinInt64,
		// Max: math.MaxInt64,
	},
	"google.protobuf.UInt32Value": {
		Type:     "integer",
		Nullable: true,
		// Min: 0,
		// Max: math.MaxUInt32,
	},
	"google.protobuf.UInt64Value": {
		Type:     "integer",
		Nullable: true,
		// Min: 0,
		// Max: math.MaxUInt62,
	},
	"google.protobuf.FloatValue": {
		Type:     "number",
		Nullable: true,
	},
	"google.protobuf.Duration": {
		Type: "string",
	},
	"google.protobuf.Empty": {
		Type:          "object",
		MaxProperties: Ptr(int64(0)),
	},
	"google.protobuf.Timestamp": {
		Type:   "string",
		Format: "date-time",
	},
}

type openapiGenerator struct {
	model *protomodel.Model

	// transient state as individual files are processed
	currentPackage *protomodel.PackageDescriptor

	messages map[string]*protomodel.MessageDescriptor

	// @solo.io customizations to limit length of generated descriptions
	descriptionConfiguration *DescriptionConfiguration

	// @solo.io customization to support enum validation schemas with int or string values
	// we need to support this since some controllers marshal enums as integers and others as strings
	enumAsIntOrString bool

	// @solo.io customizations to define schemas for certain messages
	customSchemasByMessageName map[string]*apiext.JSONSchemaProps
}

type DescriptionConfiguration struct {
	// Whether or not to include a description in the generated open api schema
	IncludeDescriptionInSchema bool
}

func newOpenAPIGenerator(
	model *protomodel.Model,
	descriptionConfiguration *DescriptionConfiguration,
	enumAsIntOrString bool,
) *openapiGenerator {
	return &openapiGenerator{
		model:                      model,
		descriptionConfiguration:   descriptionConfiguration,
		enumAsIntOrString:          enumAsIntOrString,
		customSchemasByMessageName: buildCustomSchemasByMessageName(),
	}
}

// buildCustomSchemasByMessageName name returns a mapping of message name to a pre-defined openapi schema
// It includes:
//  1. `specialSoloTypes`, a set of pre-defined schemas
func buildCustomSchemasByMessageName() map[string]*apiext.JSONSchemaProps {
	schemasByMessageName := make(map[string]*apiext.JSONSchemaProps)

	// Initialize the hard-coded values
	for name, schema := range specialSoloTypes {
		schemasByMessageName[name] = schema
	}

	return schemasByMessageName
}

func (g *openapiGenerator) generateOutput(filesToGen map[*protomodel.FileDescriptor]bool) (*plugin.CodeGeneratorResponse, error) {
	response := plugin.CodeGeneratorResponse{}

	g.generateSingleFileOutput(filesToGen, &response)

	return &response, nil
}

func (g *openapiGenerator) getFileContents(
	file *protomodel.FileDescriptor,
	messages map[string]*protomodel.MessageDescriptor,
	enums map[string]*protomodel.EnumDescriptor,
	descriptions map[string]string,
) {
	for _, m := range file.AllMessages {
		messages[g.relativeName(m)] = m
	}

	for _, e := range file.AllEnums {
		enums[g.relativeName(e)] = e
	}
	for _, v := range file.Matter.Extra {
		if _, n, f := strings.Cut(v, "schema: "); f {
			descriptions[n] = fmt.Sprintf("%v See more details at: %v", file.Matter.Description, file.Matter.HomeLocation)
		}
	}
}

func (g *openapiGenerator) generateSingleFileOutput(filesToGen map[*protomodel.FileDescriptor]bool, response *plugin.CodeGeneratorResponse) {
	messages := make(map[string]*protomodel.MessageDescriptor)
	enums := make(map[string]*protomodel.EnumDescriptor)
	descriptions := make(map[string]string)

	for file, ok := range filesToGen {
		if ok {
			g.getFileContents(file, messages, enums, descriptions)
		}
	}

	rf := g.generateFile("kubernetes/customresourcedefinitions.gen.yaml", messages, enums, descriptions)
	response.File = []*plugin.CodeGeneratorResponse_File{&rf}
}

const (
	enableCRDGenTag = "+cue-gen"
)

func cleanComments(lines []string) []string {
	out := []string{}
	var prevLine string
	for _, line := range lines {
		line = strings.Trim(line, " ")

		if line == "-->" {
			out = append(out, prevLine)
			prevLine = ""
			continue
		}

		if !strings.HasPrefix(line, enableCRDGenTag) {
			if prevLine != "" && len(line) != 0 {
				prevLine += " " + line
			}
			continue
		}

		out = append(out, prevLine)

		prevLine = line

	}
	if prevLine != "" {
		out = append(out, prevLine)
	}
	return out
}

func parseGenTags(s string) map[string]string {
	lines := cleanComments(strings.Split(s, "\n"))
	res := map[string]string{}
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		_, contents, f := strings.Cut(line, enableCRDGenTag)
		if !f {
			continue
		}
		spl := strings.SplitN(contents[1:], ":", 3)
		if len(spl) < 2 {
			log.Fatalf("invalid tag: %v", line)
		}
		val := ""
		if len(spl) > 2 {
			val = spl[2]
		}
		if _, f := res[spl[1]]; f {
			res[spl[1]] += ";;" + val
		} else {
			res[spl[1]] = val
		}
	}
	if len(res) == 0 {
		return nil
	}
	return res
}

// Generate an OpenAPI spec for a collection of cross-linked files.
func (g *openapiGenerator) generateFile(
	name string,
	messages map[string]*protomodel.MessageDescriptor,
	enums map[string]*protomodel.EnumDescriptor,
	descriptions map[string]string,
) plugin.CodeGeneratorResponse_File {
	g.messages = messages

	allSchemas := make(map[string]*apiext.JSONSchemaProps)

	// Type --> Key --> Value
	genTags := map[string]map[string]string{}

	for _, message := range messages {
		// we generate the top-level messages here and the nested messages are generated
		// inside each top-level message.
		if message.Parent == nil {
			g.generateMessage(message, allSchemas)
		}
		if gt := parseGenTags(message.Location().GetLeadingComments()); gt != nil {
			genTags[g.absoluteName(message)] = gt
		}
	}

	for _, enum := range enums {
		// when there is no parent to the enum.
		if len(enum.QualifiedName()) == 1 {
			g.generateEnum(enum, allSchemas)
		}
	}

	// Name -> CRD
	crds := map[string]*apiext.CustomResourceDefinition{}

	for name, cfg := range genTags {
		log.Println("Generating", name)
		group := cfg["groupName"]
		version := cfg["version"]
		kind := name[strings.LastIndex(name, ".")+1:]
		singular := strings.ToLower(kind)
		plural := singular + "s"
		spec := *allSchemas[name]
		if d, f := descriptions[name]; f {
			spec.Description = d
		}
		schema := &apiext.JSONSchemaProps{
			Type: "object",
			Properties: map[string]apiext.JSONSchemaProps{
				"spec": spec,
			},
		}
		names := apiext.CustomResourceDefinitionNames{
			Kind:     kind,
			ListKind: kind + "List",
			Plural:   plural,
			Singular: singular,
		}
		ver := apiext.CustomResourceDefinitionVersion{
			Name:   version,
			Served: true,
			Schema: &apiext.CustomResourceValidation{
				OpenAPIV3Schema: schema,
			},
		}

		if res, f := cfg["resource"]; f {
			for n, m := range extractKeyValue(res) {
				switch n {
				case "categories":
					names.Categories = mergeSlices(names.Categories, strings.Split(m, ","))
				case "plural":
					names.Plural = m
				case "kind":
					names.Kind = m
				case "shortNames":
					names.ShortNames = mergeSlices(names.ShortNames, strings.Split(m, ","))
				case "singular":
					names.Singular = m
				case "listKind":
					names.ListKind = m
				}
			}
		}
		name := names.Plural + "." + group
		if pk, f := cfg["printerColumn"]; f {
			pcs := strings.Split(pk, ";;")
			for _, pc := range pcs {
				if pc == "" {
					continue
				}
				column := apiext.CustomResourceColumnDefinition{}
				for n, m := range extractKeyValue(pc) {
					switch n {
					case "name":
						column.Name = m
					case "type":
						column.Type = m
					case "description":
						column.Description = m
					case "JSONPath":
						column.JSONPath = m
					}
				}
				ver.AdditionalPrinterColumns = append(ver.AdditionalPrinterColumns, column)
			}
		}
		if sr, f := cfg["subresource"]; f {
			if sr == "status" {
				ver.Subresources = &apiext.CustomResourceSubresources{Status: &apiext.CustomResourceSubresourceStatus{}}
				ver.Schema.OpenAPIV3Schema.Properties["status"] = apiext.JSONSchemaProps{
					Type:                   "object",
					XPreserveUnknownFields: Ptr(true),
				}
			}
		}
		if _, f := cfg["storageVersion"]; f {
			ver.Storage = true
		}
		if err := validateStructural(ver.Schema.OpenAPIV3Schema); err != nil {
			log.Fatalf("failed to validate %v as structural: %v", kind, err)
		}

		crd, f := crds[name]
		if !f {
			crd = &apiext.CustomResourceDefinition{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "apiextensions.k8s.io/v1",
					Kind:       "CustomResourceDefinition",
				},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: extractKeyValue(cfg["annotations"]),
					Labels:      extractKeyValue(cfg["labels"]),
					Name:        name,
				},
				Spec: apiext.CustomResourceDefinitionSpec{
					Group: group,
					Names: names,
					Scope: apiext.NamespaceScoped,
				},
				Status: apiext.CustomResourceDefinitionStatus{},
			}
		}

		crd.Spec.Versions = append(crd.Spec.Versions, ver)
		crds[name] = crd
		slices.SortFunc(crd.Spec.Versions, func(a, b apiext.CustomResourceDefinitionVersion) int {
			if a.Name < b.Name {
				return -1
			}
			return 1
		})
	}

	// sort the configs so that the order is deterministic.
	keys := maps.Keys(crds)
	slices.SortFunc(keys, func(a, b string) int {
		if crds[a].Spec.Group+a < crds[b].Spec.Group+b {
			return -1
		}
		return 1
	})

	bb := &bytes.Buffer{}
	bb.WriteString("# DO NOT EDIT - Generated by Cue OpenAPI generator based on Istio APIs.\n")
	for i, crdName := range keys {
		crd := crds[crdName]
		b, err := yaml.Marshal(crd)
		if err != nil {
			log.Fatalf("unable to marshall the output of %v to yaml", name)
		}
		b = fixupYaml(b)
		bb.Write(b)
		if i != len(crds)-1 {
			bb.WriteString("---\n")
		}
	}

	return plugin.CodeGeneratorResponse_File{
		Name:    proto.String(name),
		Content: proto.String(bb.String()),
	}
}

func mergeSlices(a []string, b []string) []string {
	have := sets.New(a...)
	for _, bb := range b {
		if !have.Has(bb) {
			a = append(a, bb)
		}
	}
	return a
}

// extractkeyValue extracts a string to key value pairs
// e.g. a=b,b=c to map[a:b b:c]
// and a=b,c,d,e=f to map[a:b,c,d e:f]
func extractKeyValue(s string) map[string]string {
	out := map[string]string{}
	if s == "" {
		return out
	}
	splits := strings.Split(s, "=")
	if len(splits) == 1 {
		out[splits[0]] = ""
	}
	if strings.Contains(splits[0], ",") {
		log.Fatalf("cannot parse %v to key value pairs", s)
	}
	nextkey := splits[0]
	for i := 1; i < len(splits); i++ {
		if splits[i] == "" || splits[i] == "," {
			log.Fatalf("cannot parse %v to key value paris, invalid value", s)
		}
		if !strings.Contains(splits[i], ",") && i != len(splits)-1 {
			log.Fatalf("cannot parse %v to key value pairs, missing separator", s)
		}
		if i == len(splits)-1 {
			out[nextkey] = strings.Trim(splits[i], "\"'`")
			continue
		}
		index := strings.LastIndex(splits[i], ",")
		out[nextkey] = strings.Trim(splits[i][:index], "\"'`")
		nextkey = splits[i][index+1:]
		if nextkey == "" {
			log.Fatalf("cannot parse %v to key value pairs, missing key", s)
		}
	}
	return out
}

const (
	statusOutput = `
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: null
  storedVersions: null`

	creationTimestampOutput = `
  creationTimestamp: null`
)

func fixupYaml(y []byte) []byte {
	// remove the status and creationTimestamp fields from the output. Ideally we could use OrderedMap to remove those.
	y = bytes.ReplaceAll(y, []byte(statusOutput), []byte(""))
	y = bytes.ReplaceAll(y, []byte(creationTimestampOutput), []byte(""))
	// keep the quotes in the output which is required by helm.
	y = bytes.ReplaceAll(y, []byte("helm.sh/resource-policy: keep"), []byte(`"helm.sh/resource-policy": keep`))
	return y
}

func (g *openapiGenerator) generateMessage(message *protomodel.MessageDescriptor, allSchemas map[string]*apiext.JSONSchemaProps) {
	if o := g.generateMessageSchema(message); o != nil {
		allSchemas[g.absoluteName(message)] = o
	}
}

func (g *openapiGenerator) generateCustomMessageSchema(message *protomodel.MessageDescriptor, customSchema *apiext.JSONSchemaProps) *apiext.JSONSchemaProps {
	o := customSchema
	o.Description = g.generateDescription(message)

	return o
}

func (g *openapiGenerator) generateMessageSchema(message *protomodel.MessageDescriptor) *apiext.JSONSchemaProps {
	// skip MapEntry message because we handle map using the map's repeated field.
	if message.GetOptions().GetMapEntry() {
		return nil
	}
	o := &apiext.JSONSchemaProps{
		Type:       "object",
		Properties: make(map[string]apiext.JSONSchemaProps),
	}
	o.Description = g.generateDescription(message)

	const CELOneOf = false

	for _, field := range message.Fields {
		fn := g.fieldName(field)
		sr := g.fieldType(field)
		o.Properties[fn] = *sr

		// Hack: allow "alt names"
		for _, an := range g.fieldAltNames(field) {
			o.Properties[an] = *sr
		}
	}

	// Generate OneOf
	// CEL can do this very cleanly but breaks in K8s: https://github.com/kubernetes/kubernetes/issues/120973
	// OpenAPI can do it with OneOf, but it gets a bit gross to represent "allow none set" as well.
	// 	 Many oneOfs do end up requiring at least one to be set, though -- perhaps we can simplify these cases.
	if CELOneOf {
		oneOfs := make([][]string, len(message.OneofDecl))
		for _, field := range message.Fields {
			// Record any oneOfs
			if field.OneofIndex != nil {
				oneOfs[*field.OneofIndex] = append(oneOfs[*field.OneofIndex], g.fieldName(field))
			}
		}
		for _, oo := range oneOfs {
			o.XValidations = append(o.XValidations, apiext.ValidationRule{
				Rule:    buildCELOneOf(oo),
				Message: fmt.Sprintf("At most one of %v should be set", oo),
			})
		}
	} else {
		oneOfs := make([]apiext.JSONSchemaProps, len(message.OneofDecl))
		for _, field := range message.Fields {
			// Record any oneOfs
			if field.OneofIndex != nil {
				oneOfs[*field.OneofIndex].OneOf = append(oneOfs[*field.OneofIndex].OneOf, apiext.JSONSchemaProps{Required: []string{g.fieldName(field)}})
			}
		}
		for i, oo := range oneOfs {
			oo.OneOf = append([]apiext.JSONSchemaProps{{Not: &apiext.JSONSchemaProps{AnyOf: oo.OneOf}}}, oo.OneOf...)
			oneOfs[i] = oo
		}
		switch len(oneOfs) {
		case 0:
		case 1:
			o.OneOf = oneOfs[0].OneOf
		default:
			o.AllOf = oneOfs
		}
	}

	applyExtraValidations(o, message, markers.DescribesType)
	return o
}

// buildCELOneOf builds a CEL expression to select oneOf the fields below
// Ex: (has(self.a) ? 1 : 0) + (has(self.b) ? 1 : 0) <= 1
func buildCELOneOf(names []string) string {
	clauses := []string{}
	for _, n := range names {
		// For each name, count how many are set
		clauses = append(clauses, fmt.Sprintf("(has(self.%v)?1:0)", n))
	}
	// We should have 0 or 1 set.
	return strings.Join(clauses, "+") + "<=1"
}

func (g *openapiGenerator) generateEnum(enum *protomodel.EnumDescriptor, allSchemas map[string]*apiext.JSONSchemaProps) {
	o := g.generateEnumSchema(enum)
	allSchemas[g.absoluteName(enum)] = o
}

func (g *openapiGenerator) generateEnumSchema(enum *protomodel.EnumDescriptor) *apiext.JSONSchemaProps {
	o := &apiext.JSONSchemaProps{Type: "string"}
	// Enum description is not used in Kubernetes
	// o.Description = g.generateDescription(enum)

	// If the schema should be int or string, mark it as such
	if g.enumAsIntOrString {
		o.XIntOrString = true
		return o
	}

	// otherwise, return define the expected string values
	values := enum.GetValue()
	for _, v := range values {
		b, _ := json.Marshal(v.GetName())
		o.Enum = append(o.Enum, apiext.JSON{Raw: b})
	}
	o.Type = "string"

	return o
}

func (g *openapiGenerator) absoluteName(desc protomodel.CoreDesc) string {
	typeName := protomodel.DottedName(desc)
	return desc.PackageDesc().Name + "." + typeName
}

// converts the first section of the leading comment or the description of the proto
// to a single line of description.
func (g *openapiGenerator) generateDescription(desc protomodel.CoreDesc) string {
	if !g.descriptionConfiguration.IncludeDescriptionInSchema {
		return ""
	}

	c := strings.TrimSpace(desc.Location().GetLeadingComments())
	if strings.Contains(c, "$hide_from_docs") {
		return ""
	}
	words := strings.Fields(c)
	for i, w := range words {
		if strings.HasSuffix(w, ".") {
			return strings.Join(words[:i+1], " ")
		}
	}
	return ""
}

func (g *openapiGenerator) fieldType(field *protomodel.FieldDescriptor) *apiext.JSONSchemaProps {
	schema := &apiext.JSONSchemaProps{}
	var isMap bool
	switch *field.Type {
	case descriptor.FieldDescriptorProto_TYPE_FLOAT, descriptor.FieldDescriptorProto_TYPE_DOUBLE:
		schema.Type = "number"
		schema.Format = "double"
		schema.Description = g.generateDescription(field)

	case descriptor.FieldDescriptorProto_TYPE_INT32, descriptor.FieldDescriptorProto_TYPE_SINT32, descriptor.FieldDescriptorProto_TYPE_SFIXED32:
		schema.Type = "integer"
		schema.Format = "int32"
		schema.Description = g.generateDescription(field)

	case descriptor.FieldDescriptorProto_TYPE_INT64, descriptor.FieldDescriptorProto_TYPE_SINT64, descriptor.FieldDescriptorProto_TYPE_SFIXED64:
		schema.Type = "integer"
		// TODO:
		// schema.Format = "int64"
		// schema.XIntOrString = true
		schema.Description = g.generateDescription(field)

	case descriptor.FieldDescriptorProto_TYPE_UINT64, descriptor.FieldDescriptorProto_TYPE_FIXED64:
		schema.Type = "integer"
		// TODO: schema.Format = "int64" schema.XIntOrString = true
		schema.Description = g.generateDescription(field)

	case descriptor.FieldDescriptorProto_TYPE_UINT32, descriptor.FieldDescriptorProto_TYPE_FIXED32:
		schema.Type = "integer"
		// TODO: schema.Format = "int32"
		schema.Description = g.generateDescription(field)

	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		schema.Type = "boolean"
		schema.Description = g.generateDescription(field)

	case descriptor.FieldDescriptorProto_TYPE_STRING:
		schema.Type = "string"
		schema.Description = g.generateDescription(field)

	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		msg := field.FieldType.(*protomodel.MessageDescriptor)
		if customSchema, ok := g.customSchemasByMessageName[g.absoluteName(msg)]; ok {
			schema = g.generateCustomMessageSchema(msg, customSchema)
		} else if msg.GetOptions().GetMapEntry() {
			isMap = true
			sr := g.fieldType(msg.Fields[1])
			schema = sr
			schema = &apiext.JSONSchemaProps{
				Type:                 "object",
				AdditionalProperties: &apiext.JSONSchemaPropsOrBool{Schema: schema},
			}

		} else {
			schema = g.generateMessageSchema(msg)
		}
		schema.Description = g.generateDescription(field)

	case descriptor.FieldDescriptorProto_TYPE_BYTES:
		schema.Type = "string"
		schema.Format = "binary"
		schema.Description = g.generateDescription(field)

	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		enum := field.FieldType.(*protomodel.EnumDescriptor)
		schema = g.generateEnumSchema(enum)
		schema.Description = g.generateDescription(field)
	}

	if field.IsRepeated() && !isMap {
		schema = &apiext.JSONSchemaProps{
			// Format: "array",
			Type:  "array",
			Items: &apiext.JSONSchemaPropsOrArray{Schema: schema},
		}
		schema.Description = schema.Items.Schema.Description
		schema.Items.Schema.Description = ""
	}

	applyExtraValidations(schema, field, markers.DescribesField)

	return schema
}

type SchemaApplier interface {
	ApplyToSchema(schema *apiext.JSONSchemaProps) error
}

func applyExtraValidations(schema *apiext.JSONSchemaProps, m protomodel.CoreDesc, t markers.TargetType) {
	for _, line := range strings.Split(m.Location().GetLeadingComments(), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "+kubebuilder:validation") && !strings.Contains(line, "+list") {
			continue
		}

		def := markerRegistry.Lookup(line, t)
		if def == nil {
			log.Fatalf("unknown validation: %v", line)
		}
		a, err := def.Parse(line)
		if err != nil {
			log.Fatalf("failed to parse: %v", line)
		}
		if err := a.(SchemaApplier).ApplyToSchema(schema); err != nil {
			log.Fatalf("failed to apply schema: %v", err)
		}
	}
}

func (g *openapiGenerator) fieldName(field *protomodel.FieldDescriptor) string {
	return field.GetJsonName()
}

func (g *openapiGenerator) fieldAltNames(field *protomodel.FieldDescriptor) []string {
	_, an, f := strings.Cut(field.Location().GetLeadingComments(), "+kubebuilder:altName=")
	if f {
		return []string{strings.Fields(an)[0]}
	}
	return nil
}

func (g *openapiGenerator) relativeName(desc protomodel.CoreDesc) string {
	typeName := protomodel.DottedName(desc)
	if desc.PackageDesc() == g.currentPackage {
		return typeName
	}

	return desc.PackageDesc().Name + "." + typeName
}

func Ptr[T any](t T) *T {
	return &t
}

func validateStructural(s *apiext.JSONSchemaProps) error {
	out := &apiextinternal.JSONSchemaProps{}
	if err := apiext.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(s, out, nil); err != nil {
		return fmt.Errorf("cannot convert v1 JSONSchemaProps to JSONSchemaProps: %v", err)
	}
	r, err := structuralschema.NewStructural(out)
	if err != nil {
		return fmt.Errorf("cannot convert to a structural schema: %v", err)
	}

	if errs := structuralschema.ValidateStructural(nil, r); len(errs) != 0 {
		return fmt.Errorf("schema is not structural: %v", errs.ToAggregate().Error())
	}

	return nil
}

var markerRegistry = func() *markers.Registry {
	registry := &markers.Registry{}
	if err := crdmarkers.Register(registry); err != nil {
		log.Fatal(err)
	}
	return registry
}()
