//nolint:tagliatelle // JSON keys follow the API's snake_case; YAML keys follow OpenAPI.
package tables

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ethpandaops/lab-backend/internal/bounds"
)

// Table summarises one table in the /tables index.
type Table struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Path is the table's list endpoint on this host.
	Path string `json:"path"`
	// PrimaryKey lists the fields in the required primary-key group: a request must filter on at least one.
	PrimaryKey []string            `json:"primary_key,omitempty"`
	Bounds     *bounds.TableBounds `json:"bounds,omitempty"`
}

// TableDetail describes one table's fields, their filters and the list endpoint's other parameters.
type TableDetail struct {
	Table

	Fields []Field `json:"fields"`
	// Parameters are the list endpoint's query parameters that filter no field, such as paging and order.
	Parameters []Parameter `json:"parameters"`
}

// Field is one column of a table's rows.
type Field struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Nullable    bool   `json:"nullable,omitempty"`
	Description string `json:"description,omitempty"`
	// Filters are the operators the list endpoint accepts for this field, each as the parameter <field>_<operator>.
	Filters []string `json:"filters,omitempty"`
}

// Parameter is a query parameter of a table's list endpoint.
type Parameter struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// Catalog is the set of tables one network's API serves.
type Catalog struct {
	tables []TableDetail
	byName map[string]int
}

// Tables returns every table's summary, sorted by name.
func (c *Catalog) Tables() []Table {
	out := make([]Table, len(c.tables))
	for i := range c.tables {
		out[i] = c.tables[i].Table
	}

	return out
}

// Table returns one table's detail.
func (c *Catalog) Table(name string) (TableDetail, bool) {
	i, ok := c.byName[name]
	if !ok {
		return TableDetail{}, false
	}

	return c.tables[i], true
}

// openAPIDoc holds the parts of the backend's OpenAPI spec the catalog reads.
type openAPIDoc struct {
	Paths map[string]struct {
		Get *operation `yaml:"get"`
	} `yaml:"paths"`
	Components struct {
		Schemas map[string]schema `yaml:"schemas"`
	} `yaml:"components"`
}

type operation struct {
	Parameters []struct {
		Name          string `yaml:"name"`
		In            string `yaml:"in"`
		Description   string `yaml:"description"`
		RequiredGroup string `yaml:"x-required-group"`
		Schema        schema `yaml:"schema"`
	} `yaml:"parameters"`
	Responses map[string]struct {
		Content map[string]struct {
			Schema schema `yaml:"schema"`
		} `yaml:"content"`
	} `yaml:"responses"`
}

type schema struct {
	Ref         string            `yaml:"$ref"`
	Type        string            `yaml:"type"`
	Format      string            `yaml:"format"`
	Description string            `yaml:"description"`
	Nullable    bool              `yaml:"nullable"`
	Items       *schema           `yaml:"items"`
	Properties  map[string]schema `yaml:"properties"`
}

// typeName names a schema's type, preferring its format (uint32 over integer) and marking arrays with [].
func (s *schema) typeName() string {
	if s.Type == "array" && s.Items != nil {
		return s.Items.typeName() + "[]"
	}

	if s.Format != "" {
		return s.Format
	}

	return s.Type
}

// Parse builds a network's catalog from the backend's OpenAPI spec. Each list endpoint (a GET path without path
// parameters whose response holds an array of rows) is a table: its fields come from the row schema, and each query
// parameter named <field>_<operator> adds that operator to the field's filters.
func Parse(network string, spec []byte) (*Catalog, error) {
	var doc openAPIDoc
	if err := yaml.Unmarshal(spec, &doc); err != nil {
		return nil, fmt.Errorf("parse openapi spec: %w", err)
	}

	c := &Catalog{byName: make(map[string]int)}

	for path, item := range doc.Paths {
		if item.Get == nil || strings.Contains(path, "{") {
			continue
		}

		rows, ok := rowSchema(item.Get, doc.Components.Schemas)
		if !ok {
			continue
		}

		name := path[strings.LastIndex(path, "/")+1:]
		c.tables = append(c.tables, buildTable(network, name, item.Get, rows))
	}

	sort.Slice(c.tables, func(i, j int) bool { return c.tables[i].Name < c.tables[j].Name })

	for i := range c.tables {
		c.byName[c.tables[i].Name] = i
	}

	return c, nil
}

// rowSchema follows a list operation's 200 response to the schema of the rows in its array property.
func rowSchema(op *operation, schemas map[string]schema) (schema, bool) {
	content, ok := op.Responses["200"].Content["application/json"]
	if !ok {
		return schema{}, false
	}

	response, ok := schemas[refName(content.Schema.Ref)]
	if !ok {
		return schema{}, false
	}

	for _, prop := range response.Properties {
		if prop.Type == "array" && prop.Items != nil && prop.Items.Ref != "" {
			rows, ok := schemas[refName(prop.Items.Ref)]

			return rows, ok
		}
	}

	return schema{}, false
}

func refName(ref string) string {
	return strings.TrimPrefix(ref, "#/components/schemas/")
}

func buildTable(network, name string, op *operation, rows schema) TableDetail {
	fieldNames := make([]string, 0, len(rows.Properties))
	for fieldName := range rows.Properties {
		fieldNames = append(fieldNames, fieldName)
	}

	sort.Strings(fieldNames)

	fields := make([]Field, len(fieldNames))
	index := make(map[string]int, len(fieldNames))

	for i, fieldName := range fieldNames {
		prop := rows.Properties[fieldName]
		fields[i] = Field{
			Name:        fieldName,
			Type:        prop.typeName(),
			Nullable:    prop.Nullable,
			Description: prop.Description,
		}
		index[fieldName] = i
	}

	detail := TableDetail{
		Table: Table{
			Name:        name,
			Description: rows.Description,
			Path:        "/api/v1/" + network + "/" + name,
		},
		Fields:     fields,
		Parameters: []Parameter{},
	}

	inPrimaryKey := make(map[string]bool)

	for _, param := range op.Parameters {
		if param.In != "query" {
			continue
		}

		field, operator := splitFilter(param.Name, fieldNames)
		if field == "" {
			detail.Parameters = append(detail.Parameters, Parameter{
				Name:        param.Name,
				Type:        param.Schema.typeName(),
				Description: param.Description,
			})

			continue
		}

		fields[index[field]].Filters = append(fields[index[field]].Filters, operator)

		if param.RequiredGroup != "" && !inPrimaryKey[field] {
			inPrimaryKey[field] = true
			detail.PrimaryKey = append(detail.PrimaryKey, field)
		}
	}

	return detail
}

// splitFilter splits a <field>_<operator> parameter name, matching the longest field name so that
// slot_start_date_time_eq belongs to slot_start_date_time rather than slot. It returns an empty field when the
// parameter filters no field.
func splitFilter(param string, fieldNames []string) (field, operator string) {
	for _, name := range fieldNames {
		if len(name) > len(field) && strings.HasPrefix(param, name+"_") {
			field = name
		}
	}

	if field == "" {
		return "", ""
	}

	return field, param[len(field)+1:]
}
