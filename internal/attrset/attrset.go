package attrset

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type Set map[string]PackageOrSet

type PackageOrSet struct {
	Package *Package `json:"package,omitempty"`
	Set     Set      `json:"set,omitempty"`
}

type recurseForDerivations struct {
	Recurse bool `json:"recurseForDerivations"`
}

func (ps *PackageOrSet) UnmarshalJSON(data []byte) error {
	var shouldRecurse recurseForDerivations

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

type Package struct {
	PName   string `json:"pname,omitempty"`
	Version string `json:"version,omitempty"`
}
