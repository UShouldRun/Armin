// Package build holds what a build stamps into armin. Name is the
// program's name, and with it the name of its config and data folders
// (~/.config/<Name>, ~/.local/share/<Name>): a test build of a branch
// (scripts/install-test.sh) sets it to armin-test, so it keeps its own
// config, sessions, memories, trees and join socket, and never touches
// the installed armin's.
//
//   go build -ldflags "-X armin/internal/build.Name=armin-test" ./cmd/armin
package build

// Name is "armin" unless the build sets it.
var Name = "armin"
