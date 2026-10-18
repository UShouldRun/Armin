// -library-sync and -library-add: copy files into the library, index
// it, and have the chat model catalog new books, then exit.

package display

import (
  "context"
  "fmt"

  "armin/internal/input"
  "armin/internal/library"
  "armin/internal/think"
)

// runLibrary is -library-sync and -library-add: copy in what was
// given, bring the index up to date, and have the model catalog the
// books the topic table lacks.
func runLibrary(
  ctx context.Context, opts *input.Options, mind *think.Mind,
) error {
  lib := mind.Library
  if lib == nil {
    return fmt.Errorf(
      "\n\t... in runLibrary:\n\t no library configured: " +
        "add library = <folder> to config.conf",
    )
  }

  if len(opts.LibraryAdd) > 0 {
    added, err := lib.Add(opts.LibraryAdd, opts.LibraryTo)
    for _, a := range added {
      fmt.Println("added " + a)
    }

    if err != nil {
      return fmt.Errorf("\n\t... in runLibrary:\n\t%w", err)
    }
  }

  return lib.Sync(ctx, library.AskWith(mind.Client), mind.Client.Model)
}
