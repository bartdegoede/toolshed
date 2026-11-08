package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/bartdegoede/toolshed/internal/clients"
	"github.com/bartdegoede/toolshed/internal/pkgs"
	"github.com/bartdegoede/toolshed/internal/registry"
)

// runUpgrade moves npx and uvx tools to the newest release of their package:
// it rewrites the pinned version in the registry and re-registers the tool
// where it's installed. Unpinned tools get pinned.
func runUpgrade(args []string) error {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "only show what would change")
	ids, err := parse(fs, args)
	if err != nil {
		return err
	}
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	explicit := len(ids) > 0
	if !explicit {
		for _, e := range reg.Entries {
			if _, ok := pkgs.Find(e); ok {
				ids = append(ids, e.ID)
			}
		}
	}
	installed := clients.Detect()
	ctx := context.Background()
	var errs []error
	for _, id := range ids {
		e, err := lookup(reg, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ref, ok := pkgs.Find(e)
		if !ok {
			if explicit {
				errs = append(errs, fmt.Errorf("%s doesn't run an npm or PyPI package", id))
			}
			continue
		}
		latest, err := pkgs.Latest(ctx, ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		from := ref.Version
		if from == "" {
			from = "unpinned"
		}
		if ref.Exact && ref.Version == latest {
			fmt.Printf("%s %s is up to date (%s)\n", dim("·"), id, latest)
			continue
		}
		change := fmt.Sprintf("%s → %s", from, latest)
		switch {
		case e.Source == registry.SourceBuiltin:
			fmt.Printf("%s %s %s: a built-in example; toolshed releases update it\n", dim("·"), id, dim(change))
			continue
		case *dry:
			fmt.Printf("%s %s %s\n", amber("↑"), bold(id), change)
			continue
		}
		args := ref.Pin(e, latest)
		if err := registry.SetUserArgs(id, args); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		e.Args = args
		if inst := installed[id]; inst != nil {
			if err := reregister(e, inst); err != nil {
				errs = append(errs, fmt.Errorf("%s: upgraded in your registry, but re-installing failed: %w", id, err))
				continue
			}
		}
		fmt.Printf("%s Upgraded %s %s\n", green("✓"), bold(id), change)
	}
	if len(ids) == 0 {
		fmt.Println(dim("No npx or uvx tools to upgrade."))
	}
	return errors.Join(errs...)
}

// pinNew pins a new npx or uvx tool to the newest release of its package and
// returns a line to show about it. If the lookup fails the tool is still
// added, unpinned, with a warning.
func pinNew(e *registry.Entry) string {
	v, err := pkgs.PinLatest(context.Background(), e)
	switch {
	case errors.Is(err, pkgs.ErrNothingToPin):
		return ""
	case err != nil:
		return fmt.Sprintf("  %s couldn't pin a version (%v), so it runs whatever is newest each time; pin it later with %s\n",
			amber("!"), err, bold("toolshed upgrade "+e.ID))
	}
	return fmt.Sprintf("  %s pinned to %s %s\n", green("✓"), v, dim("(`toolshed upgrade "+e.ID+"` moves it to a newer release)"))
}
