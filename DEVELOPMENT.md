# development

In general changes can be tested by running `make check`. This recipe
runs code generation, linter and tests.

The makefile recipes are largely similar to kubebuilder recipes, e.g.
the `install`, `run` and `deploy` recipes are originating from there and
apply to resource-broker.

Similarly the resource-broker-operator has `install-operator`,
`run-operator` and `deploy-operator` recipes.

To get an overview of all available recipes run `make help`.

There is also the `examples` make recipe which runs the examples in the
`examples` directory - however these take a bit of time to run
sequentially, so they are not included in the `check` recipe.
