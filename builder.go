package configkit

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

func NewBuilder() *Builder {
	return &Builder{}
}

type Builder struct {
	err      error
	values   Map
	validate *validator.Validate
}

func (b *Builder) Error() error {
	return b.err
}

func (b *Builder) hasError() bool {
	return b.err != nil
}

func (b *Builder) Set(k, v string) *Builder {
	if b.hasError() {
		return b
	}

	m := Map{}
	m.Set(k, v)
	return b.MergeMap(m)
}

func (b *Builder) WithValidator(v *validator.Validate) *Builder {
	if b.hasError() {
		return b
	}

	b.validate = v
	return b
}

func (b *Builder) Build(target interface{}) error {
	if err := validateIsPointerToStruct(target); err != nil {
		return err
	}

	if b.hasError() {
		return b.err
	}

	values := Map{}
	knownFields := map[string]reflect.Value{}

	err := walkStruct(
		target,
		func(path []string, f reflect.StructField, v reflect.Value) error {
			if !v.CanSet() {
				return nil
			}

			key := structKey(path)

			if canAssignConfig(v) {
				knownFields[key] = v
			}

			if tag, ok := f.Tag.Lookup("default"); ok {
				values.Set(key, tag)
			}

			if v.Type().Implements(_defaultConfigType) {
				m1 := v.Interface().(DefaultConfig).DefaultConfig()
				m2 := make(Map, len(m1))
				for k, v := range m1 {
					if key != "" {
						k = key + "__" + k
					}
					m2[k] = v
				}

				values.Merge(m2)
			}

			return nil
		})

	if err != nil {
		return err
	}

	values.Merge(b.values)

	{
		missingKeys := []string{}
		for key := range knownFields {
			if _, ok := values.Lookup(key); !ok {
				missingKeys = append(missingKeys, key)
			}
		}

		if len(missingKeys) > 0 {
			plural := ""
			if len(missingKeys) > 1 {
				plural = "s"
			}

			return fmt.Errorf(
				"missing %d configuration key%s: %s",
				len(missingKeys), plural,
				strings.Join(missingKeys, ", "))
		}
	}

	if err := resolveValueMap(values); err != nil {
		return wrapError(err, "resolve values")
	}

	for key, field := range knownFields {
		if err := values.Unmarshal(key, field.Addr().Interface()); err != nil {
			return wrapError(err, "unmarshal value")
		}
	}

	if err := b.Validator().Struct(target); err != nil {
		return err
	}

	return nil
}

func (b *Builder) MustBuild(v interface{}) {
	if err := b.Build(v); err != nil {
		panic(err)
	}
}

func (b *Builder) MergeFile(filename string) *Builder {
	if b.hasError() {
		return b
	}

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		b.err = err
		return b
	}

	return b.MergeData(data)
}

func (b *Builder) MergeData(data []byte) *Builder {
	if b.hasError() {
		return b
	}

	lines := bytes.Split(data, []byte("\n"))
	m := make(Map, len(lines))

	for _, line := range lines {
		line := bytes.TrimSpace(line)

		switch {
		case len(line) == 0:
			continue
		case line[0] == '#':
			continue
		}

		kvp := bytes.SplitN(line, []byte("="), 2)
		switch {
		case len(kvp[0]) == 0:
			continue
		case len(kvp) == 1:
			kvp = append(kvp, []byte(""))
		}

		m[string(kvp[0])] = string(kvp[1])
	}

	return b.MergeMap(m)
}

func (b *Builder) MergeEnviron(prefix string) *Builder {
	if b.hasError() {
		return b
	}

	env := os.Environ()
	m := make(Map)

	for _, x := range env {
		kvp := strings.SplitN(x, "=", 2)
		key := kvp[0]

		if !strings.HasPrefix(key, prefix) {
			continue
		}

		key = strings.TrimPrefix(key, prefix)

		if len(kvp) == 1 {
			m[key] = ""
		} else {
			m[key] = kvp[1]
		}
	}

	return b.MergeMap(m)
}

func (b *Builder) MergeMap(m Map) *Builder {
	if b.hasError() {
		return b
	}

	if b.values == nil {
		b.values = Map{}
	}

	for k, v := range m {
		b.values[k] = v
	}

	return b
}

func (b *Builder) MapValidator(f func(v *validator.Validate)) *Builder {
	if b.hasError() {
		return b
	}

	f(b.validate)
	return b
}

func (b *Builder) Validator() *validator.Validate {
	if b.validate == nil {
		return validator.New()
	}

	return b.validate
}