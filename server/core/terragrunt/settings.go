package terragrunt

import (
	"fmt"
	"os"

	"github.com/hashicorp/go-version"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
)

var localsSchema = &hcl.BodySchema{Blocks: []hcl.BlockHeaderSchema{{Type: "locals"}}}

// ReadSettings reads the atlantis_* locals of a terragrunt.hcl. Other locals
// are ignored, so they may use any Terragrunt function; the atlantis_* ones
// must be literal values because Atlantis reads them without evaluating the
// configuration.
func ReadSettings(file string) (Settings, error) {
	var s Settings
	src, err := os.ReadFile(file) // #nosec G304 -- a file inside the cloned repository
	if err != nil {
		return s, err
	}
	f, diags := hclparse.NewParser().ParseHCL(src, file)
	if diags.HasErrors() {
		return s, diags
	}
	content, _, diags := f.Body.PartialContent(localsSchema)
	if diags.HasErrors() {
		return s, diags
	}
	for _, block := range content.Blocks {
		attrs, diags := block.Body.JustAttributes()
		if diags.HasErrors() {
			return s, diags
		}
		for name, attr := range attrs {
			if err := s.set(name, attr); err != nil {
				return s, err
			}
		}
	}
	return s, nil
}

func (s *Settings) set(name string, attr *hcl.Attribute) error {
	var want cty.Type
	switch name {
	case "atlantis_skip", "atlantis_autoplan":
		want = cty.Bool
	case "extra_atlantis_dependencies":
		want = cty.List(cty.String)
	case "atlantis_terraform_version":
		want = cty.String
	default:
		return nil
	}
	v, diags := attr.Expr.Value(nil)
	if diags.HasErrors() {
		return fmt.Errorf("local %s must be a literal value: %s", name, diags.Error())
	}
	v, err := convert.Convert(v, want)
	if err != nil || v.IsNull() || !v.IsWhollyKnown() {
		return fmt.Errorf("local %s must be a %s", name, want.FriendlyName())
	}
	switch name {
	case "atlantis_skip":
		s.Skip = v.True()
	case "atlantis_autoplan":
		b := v.True()
		s.Autoplan = &b
	case "extra_atlantis_dependencies":
		for _, e := range v.AsValueSlice() {
			s.ExtraDependencies = append(s.ExtraDependencies, e.AsString())
		}
	case "atlantis_terraform_version":
		tv, err := version.NewVersion(v.AsString())
		if err != nil {
			return fmt.Errorf("local %s: %w", name, err)
		}
		s.TerraformVersion = tv
	}
	return nil
}
