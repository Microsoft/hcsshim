package main

import (
	"path/filepath"

	"github.com/Microsoft/hcsshim"
	"github.com/Microsoft/hcsshim/internal/appargs"
	"github.com/urfave/cli"
)

var makeBaseLayerCommand = cli.Command{
	Name:      "makebaselayer",
	Usage:     "converts a directory containing 'Files/' into a base layer",
	ArgsUsage: "<layer path>",
	Before:    appargs.Validate(appargs.NonEmptyString),
	Action: func(context *cli.Context) error {
		path, err := filepath.Abs(context.Args().First())
		if err != nil {
			return err
		}

		return hcsshim.ConvertToBaseLayer(path)
	},
}
