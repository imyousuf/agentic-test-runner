// Package skills holds no code. It exists so the skills shipped in the plugin
// can be tested like anything else in the repository.
//
// Skills are software. They are loaded by agents instead of compiled, but they
// are versioned with the CLI, ship in the same artefact, and go wrong in the
// same way: the binary gains a capability, the skill still describes the old
// one, and an agent reading the skill does not know the capability exists.
// :has-text is the worked example — the compile prompt learned it and the
// skills stayed silent for a release.
//
// So: a change to compiler or runtime behaviour is not complete until the
// skill changes in the same commit, and this test is what makes that
// enforceable rather than aspirational.
package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const skillsDir = "../../skills"

// skillFiles returns every markdown and template file shipped under skills/.
func skillFiles(t *testing.T) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.WalkDir(skillsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".md", ".txt":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", skillsDir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no skill files found under %s", skillsDir)
	}
	return files
}

// Each capability the binary has that an agent cannot use unless a skill says
// so. Add a row here in the same commit that adds the capability.
func TestSkillsDocumentCurrentCapabilities(t *testing.T) {
	capabilities := []struct {
		name  string
		token string
	}{
		{"text-qualified selectors", ":has-text("},
		{"regex assertions", "toMatch"},
		{"fixtures that survive a repeated compile", "atr.setup"},
		{"presence assertions that wait", "atr.expectExists"},
		{"absence assertions", "atr.expectMissing"},
		{"replay-only mode", "--no-compile"},
		{"forced recompilation", "--recompile"},
		{"repair suppression", "--no-repair"},
		{"input pruning", "--prune-values"},
		{"externalised test inputs", ".test.properties"},
		{"the freshness header", "atr-spec-sha256"},
		{"the unverified marker", "atr-unverified"},
		{"the config failure kind", "`config`"},
		{"secrets that never enter the transcript", "fillSecret"},
		{"the false-pass lint", "--lint"},
		{"execution history", "atr history"},
		{"telemetry export", "OTEL_EXPORTER_OTLP_ENDPOINT"},
		{"the shared operations library", "_shared.js"},
		{"the library freshness header", "atr-lib-sha256"},
	}

	files := skillFiles(t)

	for _, c := range capabilities {
		t.Run(c.name, func(t *testing.T) {
			for _, body := range files {
				if strings.Contains(body, c.token) {
					return
				}
			}
			t.Errorf("no skill mentions %q, so an agent reading the skills does not know this exists", c.token)
		})
	}
}

// Exit codes are the contract between ATR and a CI job. A skill that does not
// state them leaves the job unable to tell "the app is broken" from "the box
// was slow", which is how a red build stops being believed.
func TestSkillsDocumentExitCodes(t *testing.T) {
	for path, body := range skillFiles(t) {
		if strings.Contains(body, "Exit codes") || strings.Contains(body, "exit code") {
			return
		}
		_ = path
	}
	t.Error("no skill documents ATR's exit codes")
}

// Nothing naming a particular application belongs in a plugin that ships to
// everyone. The app-specific layer is the consumer's, in their own repository
// or in a spec's notes section.
func TestNoConsumerSpecificStringsInSkills(t *testing.T) {
	// Names of applications ATR has been adopted against. example.com and
	// localhost are fine: they are placeholders, not somebody's product.
	forbidden := []string{"opal", "hypatia", "sosuke"}

	for path, body := range skillFiles(t) {
		lower := strings.ToLower(body)
		for _, word := range forbidden {
			if strings.Contains(lower, word) {
				t.Errorf("%s names a specific consumer (%q); the plugin ships to everyone", path, word)
			}
		}
	}
}

// A skill without frontmatter is not loadable, and one without a description
// is never matched to a request.
func TestEverySkillHasFrontmatter(t *testing.T) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("reading %s: %v", skillsDir, err)
	}

	found := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(skillsDir, e.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s has no SKILL.md: %v", e.Name(), err)
			continue
		}
		found++

		body := string(data)
		if !strings.HasPrefix(body, "---\n") {
			t.Errorf("%s does not open with frontmatter", path)
			continue
		}
		end := strings.Index(body[4:], "\n---")
		if end < 0 {
			t.Errorf("%s has an unterminated frontmatter block", path)
			continue
		}
		front := body[4 : 4+end]
		for _, key := range []string{"name:", "description:"} {
			if !strings.Contains(front, key) {
				t.Errorf("%s frontmatter has no %s", path, key)
			}
		}
	}
	if found == 0 {
		t.Fatal("no skills found")
	}
}

// The authoring skill's first rule forbids an assertion that cannot fail. What
// it ships as a starting point has to obey it, or the recommendation and the
// artefact contradict each other — which is exactly what `atr browser record`
// used to do.
func TestShippedTemplateHasNoWeakExpectations(t *testing.T) {
	path := filepath.Join(skillsDir, "atr-author", "references", "spec-template.test.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the spec template: %v", err)
	}
	body := string(data)

	for _, phrase := range []string{"Steps completed successfully", "No console errors"} {
		if strings.Contains(body, phrase) {
			t.Errorf("the shipped template contains an assertion that cannot fail: %q", phrase)
		}
	}
	for _, section := range []string{"Expected Results:", "Notes for the compiler:", "Setup:"} {
		if !strings.Contains(body, section) {
			t.Errorf("the shipped template has no %q section", section)
		}
	}
}

// The authoring guidance is one skill, in one place. Two documents describing
// how to write a spec is the drift this package exists to catch.
func TestAuthoringGuidanceLivesInOneSkill(t *testing.T) {
	stray := filepath.Join(skillsDir, "atr-behavior", "references", "test-file-format.md")
	if _, err := os.Stat(stray); err == nil {
		t.Errorf("%s still exists; the format reference belongs to atr-author", stray)
	}

	owned := filepath.Join(skillsDir, "atr-author", "references", "test-file-format.md")
	if _, err := os.Stat(owned); err != nil {
		t.Errorf("the format reference is not under atr-author: %v", err)
	}
}
