package testutil

import (
	"fmt"
	"testing"
)

// CheckoutFixture models a mature action repository: many mutable published releases,
// an unreadable immutability setting, two missing patch releases, and missing branding.
func CheckoutFixture(t testing.TB) *GitHub {
	g := NewGitHub(t)
	groupPatches := map[[2]int]int{}
	groupShas := map[[2]int]string{}
	group := 0
	releaseCount := 0
	for major := 1; major <= 4; major++ {
		for minor := 0; minor < 6; minor++ {
			patches := 2
			if group < 7 {
				patches++
			}
			group++
			for patch := 0; patch < patches; patch++ {
				tag := versionTag(major, minor, patch)
				sha := SHA(byte('a' + releaseCount%26))
				releaseCount++
				g.Tag(sha, tag)
				opts := []func(*Release){}
				if major == 4 && minor == 5 && patch == patches-1 {
					opts = append(opts, Latest)
				}
				g.Release(tag, opts...)
				groupShas[[2]int{major, minor}] = sha
			}
			groupPatches[[2]int{major, minor}] = patches
		}
	}

	// These tags are newer than their published releases, but do not have releases of their own.
	for patch := 3; patch <= 4; patch++ {
		g.Tag(SHA('z'), versionTag(1, 0, patch))
	}
	for major := 1; major <= 4; major++ {
		for minor := 0; minor < 6; minor++ {
			key := [2]int{major, minor}
			patch := groupPatches[key] - 1
			sha := groupShas[key]
			if major == 1 && minor == 0 {
				sha = SHA('z')
				patch = 4
			}
			g.Tag(sha, versionTag(major, minor, patch), versionMinor(major, minor))
		}
		g.Tag(groupShas[[2]int{major, 5}], versionMajor(major))
	}
	g.Fail("GET", "/immutable-releases", 403, `{"message":"Resource not accessible by integration"}`, 0)
	g.File("action.yaml", "name: Checkout\ndescription: Checkout a Git repository\nruns:\n  using: node20\n  main: dist/index.js\n")
	g.File("README.md", "# Checkout\n")
	return g
}

// CleanFixture is a small repository with consistent tags, immutable releases and marketplace metadata.
func CleanFixture(t testing.TB) *GitHub {
	sha := SHA('1')
	g := NewGitHub(t).Tag(sha, "v1.0.0", "v1.0", "v1")
	g.Release("v1.0.0", Immutable, Latest)
	return g.MarketplaceFiles()
}

// FloatingOnBranchesFixture keeps patch releases as tags and floating refs as branches.
func FloatingOnBranchesFixture(t testing.TB) *GitHub {
	sha := SHA('1')
	g := NewGitHub(t).Tag(sha, "v1.0.0").Branch(sha, "v1.0", "v1")
	g.Release("v1.0.0", Immutable, Latest)
	return g.MarketplaceFiles()
}

// DuplicateRefsFixture has floating refs in both the tag and branch namespaces.
func DuplicateRefsFixture(t testing.TB) *GitHub {
	sha := SHA('1')
	g := NewGitHub(t).Tag(sha, "v1.0.0", "v1.0", "v1").Branch(sha, "v1.0", "v1")
	g.Release("v1.0.0", Immutable, Latest)
	return g.MarketplaceFiles()
}

// DraftsFixture has a patch release that still needs to be published.
func DraftsFixture(t testing.TB) *GitHub {
	sha := SHA('1')
	g := NewGitHub(t).Tag(sha, "v1.0.0", "v1.0", "v1")
	g.Release("v1.0.0", Draft)
	return g.MarketplaceFiles()
}

func versionMajor(major int) string {
	return fmt.Sprintf("v%d", major)
}

func versionMinor(major, minor int) string {
	return fmt.Sprintf("v%d.%d", major, minor)
}

func versionTag(major, minor, patch int) string {
	return fmt.Sprintf("v%d.%d.%d", major, minor, patch)
}
