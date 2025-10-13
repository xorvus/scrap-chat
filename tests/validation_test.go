package tests

import (
	"reflect"
	"testing"

	"github.com/xorvus/scrap-chat/internal/utils"
)

type TestStruct struct {
	Name        string
	Age         int
	Email       string
	Description string
	Tags        []string
}

func TestCheckEmptyFields(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []string
	}{
		{
			name: "all fields filled",
			input: TestStruct{
				Name:        "John",
				Age:         30,
				Email:       "john@example.com",
				Description: "Test",
				Tags:        []string{"tag1"},
			},
			expected: []string{},
		},
		{
			name: "some empty fields",
			input: TestStruct{
				Name:  "John",
				Email: "",
				Tags:  []string{},
			},
			expected: []string{"Email", "Description", "Tags"},
		},
		{
			name: "all empty fields",
			input: TestStruct{
				Name:        "",
				Email:       "",
				Description: "",
				Tags:        []string{},
			},
			expected: []string{"Name", "Email", "Description", "Tags"},
		},
		{
			name:     "pointer to struct",
			input:    &TestStruct{Name: "John"},
			expected: []string{"Email", "Description", "Tags"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.CheckEmptyFields(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("CheckEmptyFields() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestValidateStruct(t *testing.T) {
	tests := []struct {
		name      string
		input     interface{}
		shouldErr bool
	}{
		{
			name: "valid struct",
			input: TestStruct{
				Name:        "John",
				Age:         30,
				Email:       "john@example.com",
				Description: "Test",
				Tags:        []string{"tag1"},
			},
			shouldErr: false,
		},
		{
			name: "invalid struct with empty fields",
			input: TestStruct{
				Name:  "",
				Email: "",
			},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := utils.ValidateStruct(tt.input)
			if (err != nil) != tt.shouldErr {
				t.Errorf("ValidateStruct() error = %v, shouldErr %v", err, tt.shouldErr)
			}
		})
	}
}
