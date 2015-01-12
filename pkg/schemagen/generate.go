package schemagen

import (
	"fmt"
	"reflect"
	"strings"
)

type PackageDescriptor struct {
	GoPackage   string
	JavaPackage string
	Prefix      string
}

type schemaGenerator struct {
	types    map[reflect.Type]*JSONObjectDescriptor
	packages map[string]PackageDescriptor
	typeMap  map[reflect.Type]reflect.Type
}

func GenerateSchema(t reflect.Type, packages []PackageDescriptor, typeMap map[reflect.Type]reflect.Type) (*JSONSchema, error) {
	g := newSchemaGenerator(packages, typeMap)
	return g.generate(t)
}

func newSchemaGenerator(packages []PackageDescriptor, typeMap map[reflect.Type]reflect.Type) *schemaGenerator {
	pkgMap := make(map[string]PackageDescriptor)
	for _, p := range packages {
		pkgMap[p.GoPackage] = p
	}
	g := schemaGenerator{
		types:    make(map[reflect.Type]*JSONObjectDescriptor),
		packages: pkgMap,
		typeMap:  typeMap,
	}
	return &g
}

func getFieldName(f reflect.StructField) string {
	json := f.Tag.Get("json")
	if len(json) > 0 {
		parts := strings.Split(json, ",")
		return parts[0]
	}
	return f.Name
}

func (g *schemaGenerator) qualifiedName(t reflect.Type) string {
	pkgDesc, ok := g.packages[t.PkgPath()]
	if !ok {
		prefix := strings.Replace(t.PkgPath(), "/", "_", -1)
		prefix = strings.Replace(prefix, ".", "_", -1)
		prefix = strings.Replace(prefix, "-", "_", -1)
		return prefix + "_" + t.Name()
	} else {
		return pkgDesc.Prefix + t.Name()
	}
}

func (g *schemaGenerator) generateReference(t reflect.Type) string {
	return "#/definitions/" + g.qualifiedName(t)
}

func (g *schemaGenerator) javaType(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	pkgDesc, ok := g.packages[t.PkgPath()]
	if ok {
		return pkgDesc.JavaPackage + "." + t.Name()
	} else {
		switch t.Kind() {
		case reflect.Bool:
			return "bool"
		case reflect.Int, reflect.Int8, reflect.Int16,
			reflect.Int32, reflect.Int64, reflect.Uint,
			reflect.Uint8, reflect.Uint16, reflect.Uint32,
			reflect.Uint64:
			return "int"
		case reflect.Float32, reflect.Float64, reflect.Complex64,
			reflect.Complex128:
			return "double"
		case reflect.String:
			return "String"
		case reflect.Array, reflect.Slice:
			return "java.util.ArrayList<" + g.javaType(t.Elem()) + ">"
		case reflect.Map:
			return "java.util.Map<String," + g.javaType(t.Elem()) + ">"
		default:
			if len(t.Name()) == 0 && t.NumField() == 0 {
				return "Object"
			}
			return t.Name()
		}
	}
}

func (g *schemaGenerator) generate(t reflect.Type) (*JSONSchema, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("Only struct types can be converted.")
	}

	s := JSONSchema{
		ID:     "http://fabric8.io/fabric8/v2/" + t.Name() + "#",
		Schema: "http://json-schema.org/schema#",
		JSONDescriptor: JSONDescriptor{
			Type: "object",
		},
	}
	s.JSONObjectDescriptor = g.generateObjectDescriptor(t)
	if len(g.types) > 0 {
		s.Definitions = make(map[string]JSONPropertyDescriptor)
		for k, v := range g.types {
			name := g.qualifiedName(k)
			value := JSONPropertyDescriptor{
				JSONDescriptor: &JSONDescriptor{
					Type: "object",
				},
				JSONObjectDescriptor: v,
				JavaTypeDescriptor: &JavaTypeDescriptor{
					JavaType: g.javaType(k),
				},
			}
			s.Definitions[name] = value
		}
	}
	return &s, nil
}

func (g *schemaGenerator) getPropertyDescriptor(t reflect.Type) JSONPropertyDescriptor {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	tt, ok := g.typeMap[t]
	if ok {
		t = tt
	}
	switch t.Kind() {
	case reflect.Bool:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type: "boolean",
			},
		}
	case reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Int32, reflect.Int64, reflect.Uint,
		reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type: "integer",
			},
		}
	case reflect.Float32, reflect.Float64, reflect.Complex64,
		reflect.Complex128:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type: "number",
			},
		}
	case reflect.String:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type: "string",
			},
		}
	case reflect.Array:
	case reflect.Slice:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type: "array",
			},
			JSONArrayDescriptor: &JSONArrayDescriptor{
				Items: g.getPropertyDescriptor(t.Elem()),
			},
		}
	case reflect.Map:
		return JSONPropertyDescriptor{
			JSONDescriptor: &JSONDescriptor{
				Type: "object",
			},
			JSONMapDescriptor: &JSONMapDescriptor{
				MapValueType: g.getPropertyDescriptor(t.Elem()),
			},
			JavaTypeDescriptor: &JavaTypeDescriptor{
				JavaType: "java.util.Map<String," + g.javaType(t.Elem()) + ">",
			},
		}
	case reflect.Struct:
		definedType, ok := g.types[t]
		if !ok {
			g.types[t] = &JSONObjectDescriptor{}
			definedType = g.generateObjectDescriptor(t)
			g.types[t] = definedType
		}
		return JSONPropertyDescriptor{
			JSONReferenceDescriptor: &JSONReferenceDescriptor{
				Reference: g.generateReference(t),
			},
			JavaTypeDescriptor: &JavaTypeDescriptor{
				JavaType: g.javaType(t),
			},
		}
	}
	return JSONPropertyDescriptor{}
}

func (g *schemaGenerator) getStructProperties(t reflect.Type) map[string]JSONPropertyDescriptor {
	props := map[string]JSONPropertyDescriptor{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if len(field.PkgPath) > 0 { // Skip private fields
			continue
		}
		name := getFieldName(field)
		prop := g.getPropertyDescriptor(field.Type)
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			var newProps map[string]JSONPropertyDescriptor
			if prop.JSONReferenceDescriptor != nil {
				pType := field.Type
				if pType.Kind() == reflect.Ptr {
					pType = pType.Elem()
				}
				newProps = g.types[pType].Properties
			} else {
				newProps = prop.Properties
			}
			for k, v := range newProps {
				props[k] = v
			}
		} else {
			props[name] = prop
		}
	}
	return props
}
func (g *schemaGenerator) generateObjectDescriptor(t reflect.Type) *JSONObjectDescriptor {
	desc := JSONObjectDescriptor{AdditionalProperties: true}
	desc.Properties = g.getStructProperties(t)
	return &desc
}
