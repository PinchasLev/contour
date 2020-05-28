package envoy

import (
	"encoding/json"
	"strings"

	envoy_api_v2_route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	"github.com/golang/protobuf/ptypes/any"
	_struct "github.com/golang/protobuf/ptypes/struct"
	"github.com/projectcontour/contour/internal/dag"
	"github.com/projectcontour/contour/internal/protobuf"
)

func PerFilterConfig(r *dag.Route) (conf map[string]*_struct.Struct) {
	if r.PerFilterConfig == nil {
		return
	}

	conf = make(map[string]*_struct.Struct)
	var inInterface map[string]interface{}
	inrec, _ := json.Marshal(r.PerFilterConfig)
	json.Unmarshal(inrec, &inInterface)

	for k, v := range inInterface {
		s := new(_struct.Struct)
		conf[k] = s

		recurseIface(s, v)
	}
	return
}

func TypedPerFilterConfig(r *dag.Route) (conf map[string]*any.Any) {
	if r.PerFilterConfig == nil {
		return
	}

	conf = make(map[string]*any.Any)
	var inInterface map[string]interface{}
	inrec, _ := json.Marshal(r.PerFilterConfig)
	json.Unmarshal(inrec, &inInterface)

	for k, v := range inInterface {
		s := new(_struct.Struct)
		recurseIface(s, v)
		conf[k] = toAny(s)
	}
	return
}

// recurseIface is a *_struct.Value producing function that recurses into nested
// structures
func recurseIface(s *_struct.Struct, iface interface{}) (ret *_struct.Value) {

	switch ifaceVal := iface.(type) {
	case int:
		ret = &_struct.Value{
			Kind: &_struct.Value_NumberValue{float64(ifaceVal)},
		}
	case int32:
		ret = &_struct.Value{
			Kind: &_struct.Value_NumberValue{float64(ifaceVal)},
		}
	case int64:
		ret = &_struct.Value{
			Kind: &_struct.Value_NumberValue{float64(ifaceVal)},
		}
	case uint:
		ret = &_struct.Value{
			Kind: &_struct.Value_NumberValue{float64(ifaceVal)},
		}
	case uint32:
		ret = &_struct.Value{
			Kind: &_struct.Value_NumberValue{float64(ifaceVal)},
		}
	case uint64:
		ret = &_struct.Value{
			Kind: &_struct.Value_NumberValue{float64(ifaceVal)},
		}
	case float32:
		ret = &_struct.Value{
			Kind: &_struct.Value_NumberValue{float64(ifaceVal)},
		}
	case float64:
		ret = &_struct.Value{
			Kind: &_struct.Value_NumberValue{ifaceVal},
		}
	case string:
		ret = &_struct.Value{
			Kind: &_struct.Value_StringValue{ifaceVal},
		}
	case bool:
		ret = &_struct.Value{
			Kind: &_struct.Value_BoolValue{ifaceVal},
		}
	case map[string]interface{}:
		if s == nil { // will only be true on the initial call
			s = new(_struct.Struct)
		}
		if s.Fields == nil {
			s.Fields = make(map[string]*_struct.Value)
		}

		for k, v := range ifaceVal {
			s.Fields[k] = recurseIface(nil, v)
		}

		ret = &_struct.Value{
			Kind: &_struct.Value_StructValue{s},
		}
	case []interface{}:
		lv := new(_struct.ListValue)
		for _, v := range ifaceVal {
			lv.Values = append(lv.Values, recurseIface(nil, v))
		}
		ret = &_struct.Value{
			Kind: &_struct.Value_ListValue{lv},
		}
	default:
		ret = &_struct.Value{
			Kind: &_struct.Value_NullValue{},
		}
	}
	return
}

func setHashPolicy(r *dag.Route, ra *envoy_api_v2_route.RouteAction) {
	if len(r.HashPolicy) > 0 {
		ra.HashPolicy = make([]*envoy_api_v2_route.RouteAction_HashPolicy, len(r.HashPolicy))
	}

	for i, policy := range r.HashPolicy {
		if policy.Header != nil {
			ra.HashPolicy[i] = &envoy_api_v2_route.RouteAction_HashPolicy{
				PolicySpecifier: &envoy_api_v2_route.RouteAction_HashPolicy_Header_{
					Header: &envoy_api_v2_route.RouteAction_HashPolicy_Header{
						HeaderName: policy.Header.HeaderName,
					},
				},
			}
		} else if policy.Cookie != nil {
			policySpecifier := &envoy_api_v2_route.RouteAction_HashPolicy_Cookie_{
				Cookie: &envoy_api_v2_route.RouteAction_HashPolicy_Cookie{
					Name: policy.Cookie.Name,
					Path: policy.Cookie.Path,
				},
			}
			if policy.Cookie.Ttl != nil {
				policySpecifier.Cookie.Ttl = &policy.Cookie.Ttl.Duration
			}

			ra.HashPolicy[i] = &envoy_api_v2_route.RouteAction_HashPolicy{
				PolicySpecifier: policySpecifier,
			}
		} else if policy.ConnectionProperties != nil {
			ra.HashPolicy[i] = &envoy_api_v2_route.RouteAction_HashPolicy{
				PolicySpecifier: &envoy_api_v2_route.RouteAction_HashPolicy_ConnectionProperties_{
					ConnectionProperties: &envoy_api_v2_route.RouteAction_HashPolicy_ConnectionProperties{
						SourceIp: policy.ConnectionProperties.SourceIp,
					},
				},
			}
		}

		if ra.HashPolicy[i] != nil {
			ra.HashPolicy[i].Terminal = policy.Terminal
		}
	}
}

func adobeDefaultRetryPolicy() *envoy_api_v2_route.RetryPolicy {
	return &envoy_api_v2_route.RetryPolicy{
		RetryOn:                       "connect-failure",
		NumRetries:                    protobuf.UInt32(3),
		HostSelectionRetryMaxAttempts: 3,
	}
}

// Merges the default Adobe RetryPolicy with the Route-level policy (Envoy doesn't merge)
// https://www.envoyproxy.io/docs/envoy/v1.13.1/api-v2/api/v2/route/route_components.proto#envoy-api-field-route-virtualhost-retry-policy
// https://www.envoyproxy.io/docs/envoy/v1.13.1/api-v2/api/v2/route/route_components.proto#envoy-api-field-route-routeaction-retry-policy
func adobeRetryPolicy(r *dag.Route) *envoy_api_v2_route.RetryPolicy {
	rp := retryPolicy(r)
	// if there are no route-level policies, then there is nothing to merge: the
	// Adobe VirtualHost-level policy will apply
	if rp == nil {
		return nil
	}

	adobeDefault := adobeDefaultRetryPolicy()

	// RetryOn is a comma-separated list of strings
	retryOnValues := []string{
		rp.RetryOn,
		adobeDefault.RetryOn,
	}
	rp.RetryOn = strings.Join(retryOnValues, ",")

	// NumRetries is directly tied to PerTryTimeout - don't override the
	// client-provided value
	if rp.NumRetries == nil {
		rp.NumRetries = adobeDefault.NumRetries
	}

	// HostSelectionRetryMaxAttempts is not configured by upstream
	rp.HostSelectionRetryMaxAttempts = adobeDefault.HostSelectionRetryMaxAttempts

	return rp
}
