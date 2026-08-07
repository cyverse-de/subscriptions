package query

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
)

// Define a single validator to do all of the validations for us.
var v = validator.New()

// ValidatedQueryParam extracts a query parameter and validates it.
func ValidatedQueryParam(ctx echo.Context, name, validationTag string) (string, error) {
	value := ctx.QueryParam(name)

	// Validate the value.
	if err := v.Var(value, validationTag); err != nil {
		return "", err
	}

	return value, nil
}

// ValidateBooleanQueryParam extracts a Boolean query parameter and validates it.
func ValidateBooleanQueryParam(ctx echo.Context, name string, defaultValue *bool) (bool, error) {
	errMsg := fmt.Sprintf("invalid query parameter: %s", name)
	value := ctx.QueryParam(name)

	// Assume that the parameter is required if there's no default.
	if defaultValue == nil {
		if err := v.Var(value, "required"); err != nil {
			return false, fmt.Errorf("missing required query parameter: %s", name)
		}
	}

	// If no value was provided at this point then the prameter is optional; return the default value.
	if value == "" {
		return *defaultValue, nil
	}

	// Parse the parameter value and return the result.
	result, err := strconv.ParseBool(value)
	if err != nil {
		return false, errors.Wrap(err, errMsg)
	}
	return result, nil
}

// ValidateDateQueryParam extracts the value of a date query parameter and validates it.
func ValidateDateQueryParam(ctx echo.Context, name string, defaultValue *time.Time) (time.Time, error) {
	errMsg := fmt.Sprintf("invalid query parameter: %s", name)
	value := ctx.QueryParam(name)

	// Extract the time value.
	var timeValue time.Time

	// Assume that the parameter is required if there's no default.
	if defaultValue == nil {
		if err := v.Var(value, "required"); err != nil {
			return timeValue, fmt.Errorf("missing required query parameter: %s", name)
		}
	}

	// If no value was provided at this point then the parameter is optional; return the default value.
	if value == "" {
		return *defaultValue, nil
	}

	// Parse the parameter value and return the result.
	timeValue, err := time.ParseInLocation(time.DateOnly, value, time.Now().Location())
	if err != nil {
		return timeValue, errors.Wrap(err, errMsg)
	}

	return timeValue, nil
}

// ValidateIntQueryParam extracts an optional integer query parameter and validates it.
func ValidateIntQueryParam(ctx echo.Context, name string, defaultValue *int32, checks ...string) (int32, error) {
	errMsg := fmt.Sprintf("invalid query parameter: %s", name)
	value := ctx.QueryParam(name)
	var result int32

	// Assume that the parameter is required if there's no default.
	if defaultValue == nil && value == "" {
		return result, fmt.Errorf("missing rquired query parameter: %s", name)
	}

	// If no value was provided at this point then the parameter is optional; return the default value.
	if value == "" {
		return *defaultValue, nil
	}

	// Parse the parameter value.
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return result, errors.Wrap(err, errMsg)
	}
	result = int32(parsed)

	// Perform any checks that we're supposed to perform.
	for _, check := range checks {
		if err = v.Var(result, check); err != nil {
			return result, errors.Wrap(err, errMsg)
		}
	}

	return result, nil
}

// contains returns true if the given slice of strings contains the given string.
func contains(strs []string, str string) bool {
	for _, s := range strs {
		if s == str {
			return true
		}
	}
	return false
}

// ValidateEnumQueryParam extracts the value of an enumeration query parameter. The value will always be converted to
// lower case before validating an returning it.
func ValidateEnumQueryParam(ctx echo.Context, name string, vals []string, defaultValue *string) (string, error) {
	value := strings.ToLower(ctx.QueryParam(name))

	// Assume that the value is required if there's no default.
	if defaultValue == nil && value == "" {
		return "", fmt.Errorf("missing required query parameter: %s", name)
	}

	// If no value was provide at this point then the parameter is optionl; return the default value.
	if value == "" {
		return *defaultValue, nil
	}

	// Validate the value.
	if !contains(vals, value) {
		return "", fmt.Errorf("invalid query parameter: %s; valid values: %s", name, strings.Join(vals, ", "))
	}
	return value, nil
}

// ValidateSortDir extracts the value of a sort direction query parameter and validates it. The value will always be
// converted to lower case before validating and returning it.
func ValidateSortDir(ctx echo.Context) (string, error) {
	defaultSortDir := "asc"
	return ValidateEnumQueryParam(ctx, "sort-dir", []string{"asc", "desc"}, &defaultSortDir)
}
