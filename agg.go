package ges

import (
	"encoding/json"
)

/***************************
    @author: tiansheng.ren
    @date: 2022/12/29
    @desc:

***************************/

func AggDataHistogram(field, interval, format, offset, timeZone string) Agg {
	return agg{}.Name(field).DateHistogram(field, interval, format, offset, timeZone)
}
func AggDataHistogramName(name, field, interval, format, offset, timeZone string) Agg {
	return agg{}.Name(name).DateHistogram(field, interval, format, offset, timeZone)
}

func AggFilters(name string, subAgg Agg, filters ...AggFilter) Agg {
	return agg{}.Name(name).Filter(subAgg, filters...)
}

func AggDistinct(field string, number int64) Agg {
	return agg{}.Name(field).Distinct(field, number)
}

// agg 必须有MarshalJSON，用来生成查询es 需要条件
// 同一个agg中filter 与其他互斥
type agg struct {
	name string
	// 在项目实际使用filter 有多个metric 与 data_histogram 同时使用出现，稳定复现buckets中key 并非预期的问题
	filters       *aggFilters
	dataHistogram *aggDateHistogram `json:"date_histogram"`
	distinct      *aggDistinct      `json:"terms"`
	raw           interface{}       `json:"raw"`
}

func (a agg) MarshalJSON() ([]byte, error) {
	result := make(map[string]interface{}, 0)
	if a.filters == nil {
		if a.dataHistogram != nil {
			result["date_histogram"] = a.dataHistogram
		} else if a.distinct != nil {
			result["terms"] = a.distinct
		}
		return json.Marshal(result)
	}

	filters := make(map[string]interface{}, len(a.filters.buckets))
	for _, item := range a.filters.buckets {
		name, cond := item.Result()
		filters[name] = cond
	}
	result["filters"] = map[string]interface{}{"filters": filters}
	subAggName, subAgg := a.filters.agg.Result()
	result["aggs"] = map[string]interface{}{subAggName: subAgg}
	return json.Marshal(result)
}

func (a agg) Name(name string) Agg {
	a.name = name
	return a
}

func (a agg) Filter(agg Agg, filter ...AggFilter) Agg {
	a.filters = &aggFilters{buckets: filter, agg: agg}
	return a
}

func (a agg) DateHistogram(field, interval, format, offset, timeZone string) Agg {
	a.dataHistogram = &aggDateHistogram{
		Field:            field,
		CalendarInterval: interval,
		Format:           format,
		Offset:           offset,
		TimeZone:         timeZone,
	}
	return a
}

func (a agg) Distinct(field string, number int64) Agg {
	a.distinct = &aggDistinct{
		Field: field,
		Size:  number,
	}

	return a
}

func (a agg) Raw(raw interface{}) Agg {
	a.raw = raw
	return a
}

func (a agg) Result() (string, interface{}) {
	return a.name, a
}

type aggDateHistogram struct {
	Field string `json:"field,omitempty"`
	// minute,hour,day,week,month,quarter,year
	CalendarInterval string `json:"calendar_interval,omitempty"`
	Format           string `json:"format,omitempty"`
	Offset           string `json:"offset,omitempty"`
	TimeZone         string `json:"time_zone,omitempty"`
}

type aggDistinct struct {
	Field string `json:"field"`
	Size  int64  `json:"size"`
}

//  aggFilter
//  @Description:
//
type aggFilters struct {
	buckets []AggFilter
	agg     Agg
}

func AggFilterName(name string) AggFilter {
	return &aggFilter{
		name:  name,
		terms: nil,
	}
}

type aggFilter struct {
	name  string
	terms map[string]interface{}
}

func (a aggFilter) Name(name string) AggFilter {
	a.name = name
	return a
}

// Terms  raw value must be slice or array
func (a aggFilter) Terms(field string, val ...interface{}) AggFilter {
	if a.terms == nil {
		a.terms = make(map[string]interface{}, 0)
	}
	a.terms[field] = val
	return a
}

func (a aggFilter) TermsArray(field string, arr interface{}) AggFilter {
	if a.terms == nil {
		a.terms = make(map[string]interface{}, 0)
	}
	a.terms[field] = arr
	return a
}

func (a aggFilter) Result() (string, map[string]interface{}) {
	result := make(map[string]interface{}, 0)
	if a.terms != nil {
		result["terms"] = a.terms
	}
	return a.name, result
}

var (
	_ Agg       = (*agg)(nil)
	_ AggFilter = (*aggFilter)(nil)
)