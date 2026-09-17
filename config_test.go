package infobip_mcp

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"go.k6.io/k6/js/common"
)

// TestClientConfigJSFieldNames pins the JavaScript name of every ClientConfig
// field. k6 maps Go field names with snaker.CamelToSnake() unless an explicit
// `js` tag overrides it, which silently exposed IsSSE to scripts as "is_s_s_e"
// instead of the documented "isSSE".
func TestClientConfigJSFieldNames(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"Endpoint": "endpoint",
		"Timeout":  "timeout",
		"IsSSE":    "isSSE",
		"Headers":  "headers",
	}

	configType := reflect.TypeOf(ClientConfig{})
	require.Equal(t, len(want), configType.NumField(),
		"ClientConfig gained or lost a field without updating this test")

	for i := range configType.NumField() {
		field := configType.Field(i)
		expected, ok := want[field.Name]
		require.True(t, ok, "unexpected ClientConfig field %q", field.Name)
		require.Equal(t, expected, common.FieldName(configType, field),
			"field %q is exposed to JavaScript under the wrong name", field.Name)
	}
}
