package utils

import (
	"fmt"
	"reflect"
)

func CheckEmptyFields(info interface{}) []string {
	var emptyFields []string
	v := reflect.ValueOf(info)

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return emptyFields
	}

	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		fieldValue := v.Field(i)
		fieldName := t.Field(i).Name

		if !fieldValue.CanInterface() {
			continue
		}

		if isEmptyValue(fieldValue) {
			emptyFields = append(emptyFields, fieldName)
		}
	}

	return emptyFields
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return v.String() == ""
	case reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	default:
		return false
	}
}

func ValidateStruct(info interface{}) error {
	emptyFields := CheckEmptyFields(info)
	if len(emptyFields) > 0 {
		return fmt.Errorf("empty fields: %v", emptyFields)
	}
	return nil
}
