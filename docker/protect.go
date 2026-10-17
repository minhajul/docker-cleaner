package docker

import (
	"errors"
	"fmt"
	"path"
	"strings"

	client "github.com/fsouza/go-dockerclient"
)

// Reason says why a resource is protected from deletion.
type Reason string

const (
	// NotProtected is the Reason of a resource that may be deleted.
	NotProtected Reason = ""
	// ReasonSystem marks a resource of a system orchestration layer
	// (Kubernetes, minikube, k3s, kind); see IsSystemImage and
	// IsSystemContainer.
	ReasonSystem Reason = "system"
	// ReasonProtectList marks a resource matching a user pattern from the
	// config file's protect-list.
	ReasonProtectList Reason = "protect-list"
)

// Protector decides which resources the TUI hides and never deletes: those
// matched by the built-in system rules, plus those whose name matches one of
// the user's patterns. The zero value applies the system rules only.
//
// Patterns are globs with path.Match syntax (*, ?, [...] and \ escapes),
// except that "/" is an ordinary character, so * and ? match it too:
// "ghcr.io/acme/*" matches "ghcr.io/acme/tools/api:1.0". Matching is
// case-sensitive and a pattern must match the whole name. A leading "/" on a
// pattern is ignored, so "/my-db" means "my-db".
//
// Each resource type is matched against its names:
//   - an image against every tag ("postgres:16") and the repository of every
//     tag ("postgres"), so "postgres" and "postgres:*" both protect every
//     postgres tag while "postgres:16" protects only that one. Untagged
//     images have no names and match no pattern.
//   - a container against each of its names, without Docker's leading "/".
//
// Further resource types matched by name (volumes, networks) need one method
// each, built on matchesAny.
type Protector struct {
	patterns []string
}

// NewProtector returns a Protector that adds patterns to the system rules.
// It fails on the first invalid pattern (see ValidatePattern).
func NewProtector(patterns []string) (Protector, error) {
	p := Protector{patterns: make([]string, 0, len(patterns))}
	for _, pat := range patterns {
		if err := ValidatePattern(pat); err != nil {
			return Protector{}, err
		}
		p.patterns = append(p.patterns, normalisePattern(pat))
	}
	return p, nil
}

// ValidatePattern reports whether pattern is a usable protect-list pattern:
// non-empty and well-formed (no unclosed "[" or trailing "\").
func ValidatePattern(pattern string) error {
	if normalisePattern(pattern) == "" {
		return errors.New("empty pattern")
	}
	// path.Match checks the whole pattern even when the name does not match.
	if _, err := path.Match(globEscapeSlash(normalisePattern(pattern)), ""); err != nil {
		return fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}
	return nil
}

// ImageReason reports why img is protected, or NotProtected.
func (p Protector) ImageReason(img client.APIImages) Reason {
	if IsSystemImage(img) {
		return ReasonSystem
	}
	var names []string
	for _, tag := range img.RepoTags {
		if tag == "<none>:<none>" {
			continue
		}
		names = append(names, tag, imageRepo(tag))
	}
	if p.matchesAny(names...) {
		return ReasonProtectList
	}
	return NotProtected
}

// ContainerReason reports why c is protected, or NotProtected.
func (p Protector) ContainerReason(c client.APIContainers) Reason {
	if IsSystemContainer(c) {
		return ReasonSystem
	}
	names := make([]string, len(c.Names))
	for i, name := range c.Names {
		names[i] = strings.TrimPrefix(name, "/")
	}
	if p.matchesAny(names...) {
		return ReasonProtectList
	}
	return NotProtected
}

// ProtectsImage reports whether img must be hidden and never deleted.
func (p Protector) ProtectsImage(img client.APIImages) bool {
	return p.ImageReason(img) != NotProtected
}

// ProtectsContainer reports whether c must be hidden and never deleted.
func (p Protector) ProtectsContainer(c client.APIContainers) bool {
	return p.ContainerReason(c) != NotProtected
}

// matchesAny reports whether any user pattern matches any of names.
func (p Protector) matchesAny(names ...string) bool {
	for _, pat := range p.patterns {
		for _, name := range names {
			if globMatch(pat, name) {
				return true
			}
		}
	}
	return false
}

// imageRepo returns the repository part of an image reference, dropping the
// tag: "localhost:5000/app:1.0" becomes "localhost:5000/app". A colon before
// the last "/" belongs to a registry port, not a tag.
func imageRepo(ref string) string {
	i := strings.LastIndex(ref, ":")
	if i < 0 || strings.Contains(ref[i:], "/") {
		return ref
	}
	return ref[:i]
}

// normalisePattern drops the leading "/" Docker puts on container names, so
// a pattern copied from `docker inspect` still matches.
func normalisePattern(pattern string) string {
	return strings.TrimPrefix(pattern, "/")
}

// globSlash stands in for "/" when matching. path.Match treats "/" as a
// separator that * and ? never cross; NUL is just another character to it,
// and never occurs in Docker names or YAML strings written by hand.
const globSlash = "\x00"

// globEscapeSlash replaces every "/" in s with globSlash.
func globEscapeSlash(s string) string {
	return strings.ReplaceAll(s, "/", globSlash)
}

// globMatch reports whether name matches pattern, with "/" an ordinary
// character. pattern must have passed ValidatePattern.
func globMatch(pattern, name string) bool {
	ok, _ := path.Match(globEscapeSlash(pattern), globEscapeSlash(name))
	return ok
}
