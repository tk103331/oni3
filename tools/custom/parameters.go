package custom

import (
	"fmt"
	"regexp"

	"github.com/cloudwego/eino/schema"
	"github.com/tk103331/oni3/config"
)

var parameterName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func toolParameters(definitions []config.ToolParameter) (*schema.ParamsOneOf, error) {
	params := make(map[string]*schema.ParameterInfo, len(definitions))
	for _, param := range definitions {
		if !parameterName.MatchString(param.Name) {
			return nil, fmt.Errorf("invalid parameter name %q: use lowercase snake_case", param.Name)
		}
		if _, exists := params[param.Name]; exists {
			return nil, fmt.Errorf("duplicate parameter name %q", param.Name)
		}
		var kind schema.DataType
		switch param.Type {
		case "string":
			kind = schema.String
		case "number":
			kind = schema.Number
		case "integer":
			kind = schema.Integer
		case "boolean":
			kind = schema.Boolean
		case "array":
			kind = schema.Array
		case "object":
			kind = schema.Object
		default:
			return nil, fmt.Errorf("unsupported parameter type %q for %q", param.Type, param.Name)
		}
		params[param.Name] = &schema.ParameterInfo{Type: kind, Desc: param.Description, Required: param.Required}
	}
	return schema.NewParamsOneOfByParams(params), nil
}
