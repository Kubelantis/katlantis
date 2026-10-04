package planrisk

import "github.com/runatlantis/atlantis/server/core/typesafe"

// Aliases keep the plan risk API stable after the client moved to the
// typesafe package.
type (
	Question       = typesafe.Question
	Answer         = typesafe.Answer
	Response       = typesafe.Response
	Evaluator      = typesafe.Evaluator
	TypeSafeClient = typesafe.Client
)
