package attrset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type Set map[string]PackageOrSet

type Package struct {
	PName   string `json:"pname,omitempty"`
	Version string `json:"version,omitempty"`
}

type PackageOrSet struct {
	Package *Package `json:"package,omitempty"`
	Set     Set      `json:"set,omitempty"`
}

func (ps *PackageOrSet) UnmarshalJSON(data []byte) error {
	var shouldRecurse struct {
		Recurse bool `json:"recurseForDerivations"`
	}

	if bytes.Equal(data, []byte("true")) {
		return nil
	}

	err := json.Unmarshal(data, &shouldRecurse)
	if err != nil {
		fmt.Println(string(data))
		return err
	}

	if shouldRecurse.Recurse {
		var set Set
		err = json.Unmarshal(data, &set)
		ps.Set = set
	} else {
		var pkg *Package
		err = json.Unmarshal(data, &pkg)
		ps.Package = pkg
	}
	if err != nil {
		return err
	}

	return nil
}

func Flatten(s Set) map[string]string {
	m := make(map[string]string, length(s, 0))
	flatten(s, m)
	return m
}

func flatten(s Set, m map[string]string, prefixes ...string) {
	for attr, v := range s {
		if v.Package != nil {
			m[strings.Join(append(prefixes, attr), ".")] = v.Package.Version
		} else if len(v.Set) != 0 {
			flatten(v.Set, m, append(prefixes, attr)...)
		}
	}
}

func length(s Set, l int) int {
	for _, v := range s {
		if v.Set != nil {
			l += length(v.Set, l)
		} else if v.Package != nil {
			l++
		}
	}
	return l
}
