package emit

import (
	"strings"
	"testing"

	"github.com/enuan/enuan-soap-gen/internal/model"
)

func TestTagsEnrichOpenAPI(t *testing.T) {
	cases := []struct {
		name  string
		field *model.Field
		want  []string
		not   []string
	}{
		{
			name:  "multi-value enum gets enum and example",
			field: &model.Field{JSONName: "state", Required: true, Kind: model.FieldEnum, GoType: "soapclient.LoginStateCode", Enum: []string{"Ok", "Error", "Expired"}},
			want:  []string{`json:"state"`, `required:"true"`, `enum:"Ok,Error,Expired"`, `example:"Ok"`},
		},
		{
			name:  "enum value with comma disables the enum tag",
			field: &model.Field{JSONName: "value", Kind: model.FieldEnum, GoType: "string", Enum: []string{"a,b"}},
			want:  []string{`json:"value,omitempty"`},
			not:   []string{"enum:", "example:"},
		},
		{
			name:  "datetime gets the date-time format",
			field: &model.Field{JSONName: "when", GoType: "soap.XSDDateTime", Pointer: true},
			want:  []string{`json:"when,omitempty"`, `format:"date-time"`},
		},
		{
			name:  "datetime slice gets the date-time format",
			field: &model.Field{JSONName: "times", GoType: "[]soap.XSDDateTime", Array: true},
			want:  []string{`format:"date-time"`},
		},
		{
			name:  "xs:date and xs:time map to time.Time and get the date-time format",
			field: &model.Field{JSONName: "day", GoType: "time.Time", Pointer: true},
			want:  []string{`format:"date-time"`},
		},
		{
			name:  "plain string keeps only json and maxLength",
			field: &model.Field{JSONName: "plate", GoType: "string", Pointer: true, MaxLength: 8},
			want:  []string{`json:"plate,omitempty"`, `maxLength:"8"`},
			not:   []string{"format:", "example:", "enum:"},
		},
		{
			name: "schema facets and documentation become tags",
			field: &model.Field{
				JSONName:  "plate",
				GoType:    "*string",
				Pointer:   true,
				Doc:       "Vehicle plate in \"AA123BB\"\n  format.`x`",
				Pattern:   `[A-Z]{2}[0-9]{3}[A-Z]{2}`,
				MinLength: 7,
				MaxLength: 8,
				Minimum:   "0",
				Maximum:   "99",
				Default:   "AA000AA",
			},
			want: []string{
				`doc:"Vehicle plate in 'AA123BB' format.'x'"`,
				`pattern:"[A-Z]{2}[0-9]{3}[A-Z]{2}"`,
				`minLength:"7"`,
				`maxLength:"8"`,
				`minimum:"0"`,
				`maximum:"99"`,
				`default:"AA000AA"`,
			},
		},
	}

	v := &view{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := v.Tags(tc.field)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("Tags() = %q, missing %q", got, want)
				}
			}
			for _, not := range tc.not {
				if strings.Contains(got, not) {
					t.Errorf("Tags() = %q, must not contain %q", got, not)
				}
			}
		})
	}
}

func TestOpDescription(t *testing.T) {
	got := opDescription(&model.Operation{
		Name:       "Echo",
		SOAPAction: "http://tempuri.org/IChatService/Echo",
	})
	for _, want := range []string{
		"SOAP operation `Echo`",
		"SOAP 1.1, document/literal",
		"http://tempuri.org/IChatService/Echo",
		"502",
		"504",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("opDescription() = %q, missing %q", got, want)
		}
	}
}
