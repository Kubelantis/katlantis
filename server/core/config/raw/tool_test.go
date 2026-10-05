package raw_test

import (
	"testing"

	"github.com/runatlantis/atlantis/server/core/config/raw"
	. "github.com/runatlantis/atlantis/testing"
)

func TestToolValidation(t *testing.T) {
	tg, tf, bad := "terragrunt", "terraform", "pulumi"
	dir := "."
	Ok(t, raw.Project{Dir: &dir, Tool: &tg}.Validate())
	Ok(t, raw.Project{Dir: &dir, Tool: &tf}.Validate())
	ErrContains(t, `"pulumi" is not a supported tool; use one of terraform, terragrunt`, raw.Project{Dir: &dir, Tool: &bad}.Validate())

	id := "/.*/"
	Ok(t, raw.Repo{ID: id, Tool: &tg}.Validate())
	ErrContains(t, `"pulumi" is not a supported tool`, raw.Repo{ID: id, Tool: &bad}.Validate())
	Ok(t, raw.Repo{ID: id, AllowedOverrides: []string{"tool"}}.Validate())
}
