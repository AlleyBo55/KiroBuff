package attest

import (
	"strings"
	"testing"
)

func TestVendorMapping(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4.7":  "Claude",
		"openai-gpt-5.5":   "OpenAI",
		"gemini-2.5-pro":   "Google",
		"grok-4.5":         "xAI",
		"kimi-k2.5":        "Moonshot",
		"glm-4":            "Zhipu",
		"minimax-m2":       "MiniMax",
		"nemotron-super-3": "NVIDIA",
	}
	for id, want := range cases {
		got, err := Vendor(id)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		if got != want {
			t.Errorf("%s: got %q want %q", id, got, want)
		}
	}
}

func TestUnknownVendorIsRefusedNotGuessed(t *testing.T) {
	// A wrong vendor label in a legally significant trailer is worse than no
	// trailer at all.
	if _, err := Vendor("some-new-model-9000"); err == nil {
		t.Fatal("expected a refusal for an unmapped model")
	}
}

func TestTrailerMatchesPublishedShape(t *testing.T) {
	// The one published example is:
	//   Assisted-by: Claude:claude-3-opus coccinelle sparse
	s := Spec{Model: "claude-3-opus", Tools: []string{"coccinelle", "sparse"}}
	got, err := s.Trailer()
	if err != nil {
		t.Fatal(err)
	}
	want := "Assisted-by: Claude:claude-3-opus coccinelle sparse"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestTrailerIncludesAgent(t *testing.T) {
	s := Spec{Model: "claude-opus-4.7", Agent: "kiro-cli/loop", Tools: []string{"gofmt"}}
	got, err := s.Trailer()
	if err != nil {
		t.Fatal(err)
	}
	if got != "Assisted-by: Claude:claude-opus-4.7 kiro-cli/loop gofmt" {
		t.Errorf("got %q", got)
	}
}

func TestTrailerSanitizesTokens(t *testing.T) {
	// Tokens are space-separated, so an embedded space would read as an extra
	// tool, and a colon would break the Vendor:model parse.
	s := Spec{Model: "claude-opus-4.7", Agent: "my agent", Tools: []string{"a:b", "  ", "c\td"}}
	got, err := s.Trailer()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, ":") != 2 { // one for the key, one for Vendor:model
		t.Errorf("colons leaked into tokens: %q", got)
	}
	if strings.Contains(got, "my agent") {
		t.Errorf("space not sanitized: %q", got)
	}
	if strings.Contains(got, "   ") {
		t.Errorf("blank token emitted: %q", got)
	}
}

func TestTrailerRequiresModel(t *testing.T) {
	if _, err := (Spec{}).Trailer(); err == nil {
		t.Fatal("expected an error with no model")
	}
}

func TestParseTrailersOnlyReadsFinalBlock(t *testing.T) {
	msg := `mm: fix a leak

Note: this is prose, not a trailer.
It mentions Foo: bar in passing.

Signed-off-by: A Human <a@example.com>
Assisted-by: Claude:claude-opus-4.7 kiro-cli
`
	got := ParseTrailers(msg)
	if len(got) != 2 {
		t.Fatalf("expected 2 trailers, got %d: %+v", len(got), got)
	}
	if got[0].Key != "Signed-off-by" || got[1].Key != "Assisted-by" {
		t.Errorf("unexpected keys: %+v", got)
	}
	if got[1].Value != "Claude:claude-opus-4.7 kiro-cli" {
		t.Errorf("value: %q", got[1].Value)
	}
}

func TestProseInFinalBlockDisqualifiesIt(t *testing.T) {
	msg := `subject

Signed-off-by: A Human <a@example.com>
this line is not a trailer
`
	if got := ParseTrailers(msg); got != nil {
		t.Errorf("a non-trailer line must disqualify the block, got %+v", got)
	}
}

func TestSubjectOnlyMessageHasNoTrailers(t *testing.T) {
	if got := ParseTrailers("just a subject"); got != nil {
		t.Errorf("a lone subject is not a trailer block, got %+v", got)
	}
}

func TestAppendCreatesTrailerBlock(t *testing.T) {
	msg := "fix the thing\n\nSome body text.\n"
	out := Append(msg, "Assisted-by: Claude:claude-opus-4.7 kiro-cli")

	if !strings.Contains(out, "Some body text.\n\nAssisted-by:") {
		t.Errorf("expected a blank line before a new trailer block:\n%q", out)
	}
	if len(ParseTrailers(out)) != 1 {
		t.Errorf("trailer not parseable after append:\n%q", out)
	}
}

func TestAppendJoinsExistingTrailerBlock(t *testing.T) {
	msg := "fix\n\nbody\n\nSigned-off-by: A Human <a@example.com>\n"
	out := Append(msg, "Assisted-by: Claude:claude-opus-4.7")

	trailers := ParseTrailers(out)
	if len(trailers) != 2 {
		t.Fatalf("expected 2 trailers, got %d:\n%q", len(trailers), out)
	}
	if strings.Contains(out, "<a@example.com>\n\nAssisted-by") {
		t.Errorf("should not open a second block:\n%q", out)
	}
}

func TestAppendIsIdempotent(t *testing.T) {
	trailer := "Assisted-by: Claude:claude-opus-4.7"
	out := Append("fix\n\nbody\n", trailer)
	twice := Append(out, trailer)

	if strings.Count(twice, "Assisted-by") != 1 {
		t.Errorf("trailer duplicated:\n%q", twice)
	}
}

func TestValidateRequiresAttributionWhenAIAssisted(t *testing.T) {
	msg := "fix\n\nbody\n\nSigned-off-by: A Human <a@example.com>\n"

	problems := Validate(msg, Policy{AIAssisted: true})
	if len(problems) != 1 || problems[0].Rule != "missing-attribution" {
		t.Fatalf("expected missing-attribution, got %+v", problems)
	}
	// Not AI assisted: nothing required.
	if got := Validate(msg, Policy{}); len(got) != 0 {
		t.Errorf("expected no problems, got %+v", got)
	}
}

func TestValidateBlocksAgentSignOff(t *testing.T) {
	// Principle 1: only a human may certify the DCO.
	msg := "fix\n\nbody\n\nSigned-off-by: Some Agent <bot@example.com>\n"

	problems := Validate(msg, Policy{AgentMayNotSignOff: true})
	if len(problems) == 0 {
		t.Fatal("expected a violation")
	}
	var found bool
	for _, p := range problems {
		if p.Rule == "agent-signed-off" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected agent-signed-off, got %+v", problems)
	}
}

func TestValidateFlagsMalformedAttribution(t *testing.T) {
	// A trailer that looks compliant but lacks Vendor:model is worse than none.
	msg := "fix\n\nbody\n\nAssisted-by: some-model\n"

	problems := Validate(msg, Policy{AIAssisted: true})
	var found bool
	for _, p := range problems {
		if p.Rule == "malformed-attribution" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected malformed-attribution, got %+v", problems)
	}
}

func TestValidatePassesACompliantMessage(t *testing.T) {
	msg := `mm/slab: avoid double free on error path

The error path released the object twice when the allocation failed.

Signed-off-by: A Human <a@example.com>
Assisted-by: Claude:claude-opus-4.7 kiro-cli/loop sparse
`
	if got := Validate(msg, Policy{AIAssisted: true}); len(got) != 0 {
		t.Errorf("compliant message should pass, got %+v", got)
	}
}

func TestHas(t *testing.T) {
	msg := "fix\n\nbody\n\nAssisted-by: Claude:x\n"
	if !Has(msg, "Assisted-by") {
		t.Error("should find the trailer")
	}
	if !Has(msg, "assisted-by") {
		t.Error("key match should be case-insensitive")
	}
	if Has(msg, "Signed-off-by") {
		t.Error("should not find an absent trailer")
	}
}
