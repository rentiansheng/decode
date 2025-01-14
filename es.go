package ges

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"reflect"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v7/esapi"
	"github.com/rentiansheng/mapper"
)

/***************************
    @author: tiansheng.ren
    @date: 2022/11/3
    @desc:

***************************/

type es struct {
	isAgg bool
	// 	  field:desc
	sorts              []string
	fields             []string
	indexName          string
	from               uint64
	size               uint64
	cond               cond
	agg                map[string]interface{}
	adjustPureNegative bool
}

type cond struct {
	must   []interface{}
	filter []interface{}
	should []interface{}
	not    []interface{}
	exists []interface{}
}

func (e es) MarshalJSON() ([]byte, error) {
	cond := esCondition{
		Query: esConditionQuery{Bool: esQueryBool{
			Must:               e.cond.must,
			Not:                e.cond.not,
			Should:             e.cond.should,
			AdjustPureNegative: e.adjustPureNegative,
		}},
		Agg: e.agg,
	}
	queryBody := &bytes.Buffer{}
	if err := json.NewEncoder(queryBody).Encode(cond); err != nil {
		return nil, err
	}
	return queryBody.Bytes(), nil
}

func (e es) IndexName(name string) Client {
	e.indexName = name
	return e
}

func (e es) Clone() es {
	newE := es{}
	if err := mapper.AllMapper(context.TODO(), e, &newE); err != nil {
		panic("clone es error" + fmt.Sprintf("%#v", err))
	}
	return newE
}

func (e es) Index() Index {
	index := esIndex{name: e.indexName}
	return index
}

func (e es) AdjustPurePegative(v bool) Client {
	e = e.Clone()
	e.adjustPureNegative = v
	return e
}

func (e es) Not(filters ...Filter) Client {
	e = e.Clone()
	for _, filter := range filters {
		if filter == nil {
			continue
		}
		e.cond.not = append(e.cond.not, filter.Result()...)
	}
	return e
}

func (e es) Where(filters ...Filter) Client {
	e = e.Clone()
	for _, filter := range filters {
		if filter == nil {
			continue
		}
		e.cond.must = append(e.cond.must, filter.Result()...)

	}
	return e
}

func (e es) Or(filters ...Filter) Client {
	e = e.Clone()
	for _, filter := range filters {
		if filter == nil {
			continue
		}
		e.cond.should = append(e.cond.should, filter.Result()...)
	}
	return e
}

func (e es) OrderBy(field string, isDesc bool) Client {
	e = e.Clone()
	if isDesc {
		e.sorts = append(e.sorts, field+":desc")
	} else {
		e.sorts = append(e.sorts, field+":asc")
	}
	return e
}

func (e es) Agg(aggs ...Agg) Client {
	e = e.Clone()
	e.isAgg = true
	for _, agg := range aggs {
		name, value := agg.Result()
		e.agg[name] = value
	}
	return e
}

func (e es) Size(u uint64) Client {
	e = e.Clone()
	e.size = u
	return e
}

func (e es) Start(u uint64) Client {
	e = e.Clone()
	e.from = u
	return e
}

func (e es) Limit(from uint64, size uint64) Client {
	e = e.Clone()
	e.from, e.size = from, size
	return e
}

func (e es) Limit64(from int64, size int64) Client {
	e = e.Clone()
	e.from, e.size = uint64(from), uint64(size)
	return e
}

func (e es) Find(ctx context.Context, result interface{}) error {
	//TODO implement me
	panic("implement me")
}

func (e es) Search(ctx context.Context, result interface{}) (uint64, error) {
	res, err := e.searchHelper(ctx)
	if err != nil {
		return 0, fmt.Errorf("unexpected error when get: %s", err)
	}
	defer res.Body.Close()

	return e.parseSearchRespResult(ctx, res, result)
}

func (e es) SearchResultHits(ctx context.Context) ([]SearchResultHitResult, uint64, error) {
	res, err := e.searchHelper(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("unexpected error when get: %s", err)
	}
	defer res.Body.Close()

	resp, err := parseSearchRespDefaultDecode(ctx, res)
	if err != nil {
		return nil, 0, err
	}

	return resp.Hits.IndexHits, resp.Hits.Total.Value, nil
}

func (e es) searchHelper(ctx context.Context) (*esapi.Response, error) {

	queryBody, err := e.buildQuery(ctx)
	if err != nil {
		return nil, err
	}

	searchOpts := []func(*esapi.SearchRequest){
		rawESClient.Search.WithContext(ctx),
		rawESClient.Search.WithIndex(e.indexName),
		rawESClient.Search.WithSort(e.sorts...),
	}
	if queryBody.Len() > 0 {
		searchOpts = append(searchOpts, rawESClient.Search.WithBody(queryBody))

	}
	if e.isAgg {
		searchOpts = append(searchOpts, rawESClient.Search.WithSize(0))
	} else {
		searchOpts = append(searchOpts, rawESClient.Search.WithTrackTotalHits(true))
		searchOpts = append(searchOpts, rawESClient.Search.WithSourceIncludes(e.fields...))
		if e.from != 0 {
			searchOpts = append(searchOpts, rawESClient.Search.WithFrom(int(e.from)))
		}
		if e.size != 0 {
			searchOpts = append(searchOpts, rawESClient.Search.WithSize(int(e.size)))
		}
	}

	return rawESClient.Search(
		searchOpts...,
	)
}

func (e es) buildQuery(ctx context.Context) (*bytes.Buffer, error) {
	cond := esCondition{
		Query: esConditionQuery{Bool: esQueryBool{
			Must:               e.cond.must,
			Not:                e.cond.not,
			Should:             e.cond.should,
			AdjustPureNegative: e.adjustPureNegative,
		}},
		Agg: e.agg,
	}
	queryBody := &bytes.Buffer{}
	if err := json.NewEncoder(queryBody).Encode(cond); err != nil {
		return nil, fmt.Errorf("search condition build error. %s", err.Error())
	}

	return queryBody, nil
}

func (e es) TranslateSQL(ctx context.Context, sql string) ([]byte, error) {
	res, err := rawESClient.SQL.Translate(
		strings.NewReader(fmt.Sprintf(`{"query": "%s"}`, sql)),
		rawESClient.SQL.Translate.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if res.IsError() {
		return nil, fmt.Errorf(res.String())
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	return body, nil
}

func (e es) RawSQL(ctx context.Context, sql string, result interface{}) error {
	//https://www.elastic.co/guide/en/elasticsearch/reference/current/sql-search-api.html
	res, err := rawESClient.SQL.Query(strings.NewReader(fmt.Sprintf(`{"query":"%s"}`, sql)), rawESClient.SQL.Query.WithContext(ctx))
	if err != nil {
		return err
	}

	if res.IsError() {
		return fmt.Errorf(res.String())
	}
	defer res.Body.Close()

	return json.NewDecoder(res.Body).Decode(result)
}

func (e es) GetById(ctx context.Context, id string, result interface{}) error {

	res, err := rawESClient.GetSource(
		e.indexName,
		id,
		rawESClient.GetSource.WithContext(ctx),
		rawESClient.GetSource.WithPretty(),
		rawESClient.GetSource.WithSourceIncludes(e.fields...),
	)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return e.parseSourceRespResult(ctx, id, res, result)

}

// UpdateById
func (e es) UpdateById(ctx context.Context, id string, data interface{}) error {
	bufferBody := bytes.NewBufferString(fmt.Sprintf(`{"update": {"_id": "%s"}}`, id))
	bufferBody.WriteString("\n")

	// encode 会自动加上换行符
	if err := json.NewEncoder(bufferBody).Encode(mapStrAny{"doc": data}); err != nil {
		return fmt.Errorf("update by id, marshal data  fail, err: %s", err.Error())
	}

	res, err := rawESClient.Bulk(
		bufferBody,
		rawESClient.Bulk.WithIndex(e.indexName),
		rawESClient.Bulk.WithTimeout(5*time.Second),
		rawESClient.Bulk.WithContext(ctx),
		rawESClient.Bulk.WithRefresh("true"))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	_, err = parseBulkResp(ctx, res)
	if err != nil {
		return err
	}

	return nil
}

// MUpdateById
func (e es) MUpdateById(ctx context.Context, docs ...Document) error {
	bufferBody := &bytes.Buffer{}

	if len(docs) > MaxBulkUpdateItemsLimit {
		return fmt.Errorf("multi-update support max %v items", BulkItemsLimit)
	}
	jd := json.NewEncoder(bufferBody)
	for _, doc := range docs {
		id, data := doc.Item()
		bufferBody.WriteString(fmt.Sprintf(`{"update": {"_id": "%s"}}`, id))
		bufferBody.WriteString("\n")
		// encode 会自动加上换行符
		if err := jd.Encode(mapStrAny{"doc": data}); err != nil {
			return fmt.Errorf("update by id, marshal data  fail, id: %s, err: %s", id, err.Error())
		}
	}

	res, err := rawESClient.Bulk(
		bufferBody,
		rawESClient.Bulk.WithTimeout(5*time.Second),
		rawESClient.Bulk.WithContext(ctx),
		rawESClient.Bulk.WithRefresh("true"))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	_, err = parseBulkResp(ctx, res)
	if err != nil {
		return err
	}

	return nil
}

// MUpsertById  map[_id] document
func (e es) MUpsertById(ctx context.Context, docs ...Document) error {
	bufferBody := &bytes.Buffer{}

	if len(docs) > MaxBulkUpdateItemsLimit {
		return fmt.Errorf("multi-upsert support max %v items", BulkItemsLimit)
	}
	jd := json.NewEncoder(bufferBody)
	for _, doc := range docs {
		id, data := doc.Item()
		//var newData interface{}
		if id == "" {
			bufferBody.WriteString(`{"index": {}}`)

		} else {
			bufferBody.WriteString(fmt.Sprintf(`{"update": {"_id": "%s"}}`, id))
			data = mapStrAny{"doc": data, "doc_as_upsert": true}
		}
		bufferBody.WriteString("\n")
		// encode 会自动加上换行符
		if err := jd.Encode(data); err != nil {
			return fmt.Errorf("upsert by id, marshal data  fail, id: %s, err: %s", id, err.Error())
		}

	}

	res, err := rawESClient.Bulk(
		bufferBody,
		rawESClient.Bulk.WithIndex(e.indexName),
		rawESClient.Bulk.WithTimeout(5*time.Second),
		rawESClient.Bulk.WithContext(ctx),
		rawESClient.Bulk.WithRefresh("true"))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	_, err = parseBulkResp(ctx, res)
	if err != nil {
		return err
	}

	return nil
}

// UpsertById  if id  exist update document, not create document
func (e es) UpsertById(ctx context.Context, id string, doc interface{}) error {
	return e.MUpsertById(ctx, NewDoc(id, doc))
}

// USave
func (e es) USave(ctx context.Context, docs ...Document) error {
	bufferBody := &bytes.Buffer{}

	if len(docs) > MaxBulkUpdateItemsLimit {
		return fmt.Errorf("usave support max %v items", BulkItemsLimit)
	}
	jd := json.NewEncoder(bufferBody)
	for _, doc := range docs {
		var newData interface{}
		id := doc.ID()
		data := doc.Doc()
		if id == "" {
			bufferBody.WriteString(`{"index": {}}`)
			newData = data
		} else {
			bufferBody.WriteString(fmt.Sprintf(`{"update": {"_id": "%s"}}`, id))
			newData = mapStrAny{"doc": data}
		}
		bufferBody.WriteString("\n")
		// encode 会自动加上换行符
		if err := jd.Encode(newData); err != nil {
			return fmt.Errorf("USave by id, marshal data  fail, id: %s, err: %s", id, err.Error())
		}

	}

	res, err := rawESClient.Bulk(
		bufferBody,
		rawESClient.Bulk.WithIndex(e.indexName),
		rawESClient.Bulk.WithTimeout(5*time.Second),
		rawESClient.Bulk.WithContext(ctx),
		rawESClient.Bulk.WithRefresh("true"))
	if err != nil {
		return err
	}
	defer res.Body.Close()

	_, err = parseBulkResp(ctx, res)
	if err != nil {
		return err
	}

	return nil
}

func (e es) DeleteById(ctx context.Context, ids ...string) error {
	return e.Where(Terms("_id", ids)).
		Delete(ctx)
}

// Delete delete_by_query
func (e es) Delete(ctx context.Context) error {

	queryBody, err := e.buildQuery(ctx)
	if err != nil {
		return err
	}

	res, err := rawESClient.DeleteByQuery(
		[]string{e.indexName},
		queryBody,
		rawESClient.DeleteByQuery.WithTimeout(20*time.Second),
		rawESClient.DeleteByQuery.WithRefresh(true),
	)
	if err != nil {
		return fmt.Errorf("unexpected error when get: %s", err)
	}
	defer res.Body.Close()

	result := make([]interface{}, 0, 1)
	_, err = e.parseSearchRespResult(ctx, res, &result)
	return err
}

func (e es) Count(ctx context.Context) (uint64, error) {
	e.size = 0
	queryBody, err := e.buildQuery(ctx)
	if err != nil {
		return 0, err
	}
	opts := []func(*esapi.CountRequest){
		rawESClient.Count.WithIndex(e.indexName),
		rawESClient.Count.WithContext(ctx),
		rawESClient.Count.WithBody(queryBody),
	}
	res, err := rawESClient.Count(opts...)
	if err != nil {
		return 0, err
	}
	if res.IsError() {
		return 0, fmt.Errorf(res.String())
	}
	defer res.Body.Close()
	return e.parseCountRespResult(ctx, res.Body)
}

// Query raw dsl query
func (e es) Query(ctx context.Context, raw interface{}, result interface{}) error {
	body := &bytes.Buffer{}
	if err := json.NewEncoder(body).Encode(raw); err != nil {
		return err
	}
	opts := []func(*esapi.SearchRequest){
		rawESClient.Search.WithBody(body),
		rawESClient.Search.WithContext(ctx),
		rawESClient.Search.WithIndex(e.indexName),
	}
	res, err := rawESClient.Search(opts...)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf(res.String())
	}

	return json.NewDecoder(res.Body).Decode(result)
}

func (e es) Fields(fields ...string) Client {
	e.fields = fields
	return e
}

func (e es) Save(ctx context.Context, datas ...interface{}) error {

	if len(datas) > MaxBulkItemsLimit {
		return fmt.Errorf("batch insert support max %v items", BulkItemsLimit)
	}

	opts := []func(request *esapi.BulkRequest){
		rawESClient.Bulk.WithContext(ctx),
		rawESClient.Bulk.WithIndex(e.indexName),
		rawESClient.Bulk.WithRefresh("true"),
		rawESClient.Bulk.WithTimeout(20 * time.Second),
	}

	length := len(datas)
	bulkInsertAction := `{"index": {}}` + "\n"

	for now := 0; now < length; now += BulkItemsLimit {
		var items []interface{}
		if now+BulkItemsLimit > length {
			items = datas[now:length]
		} else {
			items = datas[now : now+BulkItemsLimit]
		}

		byteBody := &bytes.Buffer{}
		jd := json.NewEncoder(byteBody)
		for _, item := range items {
			byteBody.WriteString(bulkInsertAction)
			// json encode 会自动加\n
			if err := jd.Encode(item); err != nil {
				return fmt.Errorf("ges save encode data error. %s", err.Error())
			}
		}
		if byteBody.Len() == 0 {
			continue
		}
		res, err := rawESClient.Bulk(byteBody, opts...)
		if err != nil {
			return err
		}
		defer res.Body.Close()

		_, err = parseBulkResp(ctx, res)
		if err != nil {
			return err
		}
	}

	return nil

}

func ES() Client {
	return &es{
		isAgg:     false,
		sorts:     []string{},
		fields:    nil,
		indexName: "",
		from:      0,
		size:      0,
		cond:      cond{},
		agg:       make(map[string]interface{}, 0),
	}
}

func (e es) parseSearchRespResult(ctx context.Context, res *esapi.Response, results interface{}) (uint64, error) {

	resultV := reflect.ValueOf(results)
	if resultV.Kind() != reflect.Ptr {
		return 0, fmt.Errorf("results argument must be pointer")
	}
	resp, err := parseSearchRespDefaultDecode(ctx, res)
	if err != nil {
		return 0, err
	}

	total := uint64(0)
	if e.isAgg {
		if err := mapper.Mapper(ctx, resp.Aggregations, results); err != nil {
			return 0, err
		}

	} else {
		total = resp.Hits.Total.Value
		switch resultV.Elem().Kind() {
		case reflect.Slice:
			if err := e.parseSearchRespResultArray(ctx, resp, resultV); err != nil {
				return 0, err
			}
		case reflect.Map:
			if len(resp.Hits.IndexHits) == 0 {
				return 0, NotFoundError
			}
			if resultV.Elem().IsNil() {
				return 0, fmt.Errorf("results argument must be initialized")
			}
			indexHit := resp.Hits.IndexHits[0]
			if err := e.parseSearchResultIndexHit(ctx, indexHit.Id, indexHit.Source, resultV.Elem()); err != nil {
				return 0, err
			}
		case reflect.Struct:
			if len(resp.Hits.IndexHits) == 0 {
				return 0, NotFoundError
			}
			if resultV.IsNil() {
				return 0, fmt.Errorf("results argument must be initialized")
			}
			indexHit := resp.Hits.IndexHits[0]
			if err := e.parseSearchResultIndexHit(ctx, indexHit.Id, indexHit.Source, resultV); err != nil {
				return 0, err
			}
		default:
			return 0, fmt.Errorf("results argument must be a slice or map/struct address")
		}

	}
	return total, nil
}

func (e es) parseSearchRespResultArray(ctx context.Context, resp SearchResult, resultV reflect.Value) error {
	if resultV.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("results argument must be a slice address")
	}

	elemt := resultV.Elem().Type().Elem()
	slice := reflect.MakeSlice(resultV.Elem().Type(), 0, 10)
	for _, indexHit := range resp.Hits.IndexHits {
		elem := reflect.New(elemt)
		err := e.parseSearchResultIndexHit(ctx, indexHit.Id, indexHit.Source, elem)
		if err != nil {
			return err
		}
		slice = reflect.Append(slice, elem.Elem())
	}
	resultV.Elem().Set(slice)
	return nil
}

func (e es) parseSourceRespResult(ctx context.Context, id string, res *esapi.Response, result interface{}) error {

	if res.StatusCode == 404 {
		return NotFoundError
	}

	if res.IsError() {
		return fmt.Errorf("get source fail, status_code: %d, body: %s", res.StatusCode, res.String())
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("get source fail. read body %s", err.Error())
	}
	resultV := reflect.ValueOf(result)
	return e.parseSearchResultIndexHit(ctx, id, resBody, resultV)
}

func (e es) parseSearchResultIndexHit(ctx context.Context, esID string, dataRaw json.RawMessage, elemp reflect.Value) error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("recover", r)
		}
	}()
	d := json.NewDecoder(bytes.NewReader(dataRaw))
	d.UseNumber()
	if err := d.Decode(elemp.Interface()); nil != err {
		return err
	}
	for elemp.Kind() == reflect.Ptr && elemp.Elem().Kind() != reflect.Struct && elemp.Elem().Kind() != reflect.Map {
		elemp = elemp.Elem()
	}
	// add _id
	if elemp.Elem().Kind() == reflect.Map {
		elemp.Elem().SetMapIndex(reflect.ValueOf("_id"), reflect.ValueOf(esID))
	} else if elemp.Elem().Kind() == reflect.Struct {
		if !elemp.IsValid() {
			return fmt.Errorf("struct IsValid false")
		}
		if !elemp.Elem().CanSet() {
			return fmt.Errorf("struct not allow change")
		}
		elemt := elemp.Elem().Type()

		for i := 0; i < elemt.NumField(); i++ {
			field := elemt.Field(i)
			tags := strings.Split(field.Tag.Get("json"), ",")
			for _, tag := range tags {
				if tag == "_id" || tag == "es_id" {
					elemp.Elem().Field(i).Set(reflect.ValueOf(esID))
				}
			}

		}
	}

	return nil
}

func (e es) parseCountRespResult(ctx context.Context, respBody io.ReadCloser) (uint64, error) {
	var resp CountResult
	d := json.NewDecoder(respBody)
	d.UseNumber()
	err := d.Decode(&resp)
	if err != nil {
		return 0, err
	}

	if resp.Error != nil {
		return 0, fmt.Errorf("%s", resp.Error)
	}

	return resp.Count, nil
}

var _ Client = (*es)(nil)
