package data

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestChangedRoleIDsAreDeterministicAndVersionScoped(t *testing.T) {
	current, err := json.Marshal(persistedWorld{Roles: map[string]persistedRole{
		"role_b": {ID: "role_b", StateVersion: 1},
		"role_a": {ID: "role_a", StateVersion: 3},
		"role_d": {ID: "role_d", StateVersion: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	next, err := json.Marshal(persistedWorld{Roles: map[string]persistedRole{
		"role_b": {ID: "role_b", StateVersion: 2},
		"role_a": {ID: "role_a", StateVersion: 3},
		"role_c": {ID: "role_c", StateVersion: 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	roleIDs, err := changedRoleIDs(current, next)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"role_b", "role_c", "role_d"}
	if !reflect.DeepEqual(roleIDs, want) {
		t.Fatalf("changed role IDs = %#v, want %#v", roleIDs, want)
	}
}
