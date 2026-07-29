module github.com/besmpl/ember/mixedcompiler

go 1.26

require (
	example.com/ember-external-compiler v0.0.0
	github.com/besmpl/ember v0.0.0
)

replace example.com/ember-external-compiler => ../externalcompiler

replace github.com/besmpl/ember => ../../..
