package event

import (
	"fmt"
	"reflect"
)

// RequireComplete panics if any field of bundle (a struct of publisher
// interfaces, e.g. patient.EventPublishers or staff.EventPublishers) is
// nil. Those bundles are built as struct literals in main.go — a field
// left unset there compiles fine but leaves that one publisher a nil
// interface, which only panics the first time that specific event fires
// (nil.Publish(...)), possibly a rare code path that goes unnoticed for a
// long time. Calling this from each service's constructor turns that into
// an immediate, loud failure at startup instead.
func RequireComplete(bundle any) {
	v := reflect.ValueOf(bundle)
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsNil() {
			panic(fmt.Sprintf("event: %s.%s is nil — every event publisher must be wired in main.go", t.Name(), t.Field(i).Name))
		}
	}
}
