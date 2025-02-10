package test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
)

type ValueFormatter = func(interface{}) interface{}

var NoFormatting = func(v interface{}) interface{} {
	return v
}

var ConstantDistributionFormatter = func(value interface{}) interface{} {
	return int(value.(Distribution).Delegate.(*constantDistribution).Value)
}

var JsonFormatter = func(v interface{}) interface{} {
	data, err := json.Marshal(v)
	if err != nil {
		return "error"
	}
	return string(data)
}

type ColumnConfig struct {
	Header    string
	Formatter ValueFormatter
}

type ResultOptions struct {
	Columns []*ColumnConfig
}

func (o *ResultOptions) validate() error {
	if o.totalFields() == 0 {
		return errors.New("no headers")
	}
	return nil
}

func (o *ResultOptions) totalFields() int {
	return len(o.Columns)
}

type ResultOutput struct {
	options *ResultOptions
	file    *os.File
}

const fileTimeFormat = "2006-01-02 15:04:05"

func Open(name string, options *ResultOptions) *ResultOutput {
	err := options.validate()
	if err != nil {
		panic(err)
	}
	filename := fmt.Sprintf("%s-%s.txt", name, time.Now().Format(fileTimeFormat))
	file, err := utils.OverwriteFile(filename)
	if err != nil {
		panic(err)
	}
	o := &ResultOutput{file: file, options: options}
	err = o.writeHeaders()
	if err != nil {
		panic(err)
	}
	return o
}

func (o *ResultOutput) Close() error {
	return o.file.Close()
}

func (o *ResultOutput) writeHeaders() error {
	headers := make([]interface{}, o.options.totalFields())
	for i, column := range o.options.Columns {
		headers[i] = column.Header
	}
	return o.record(headers...)
}

func (o *ResultOutput) Record(values ...interface{}) error {
	if len(values) != o.options.totalFields() {
		return errors.New("not enough values passed")
	}
	results := make([]interface{}, o.options.totalFields())
	for i, value := range values {
		results[i] = o.options.Columns[i].Formatter(value)
	}
	return o.record(results...)
}

const (
	valueSeparator     = ", "
	dataPointSeparator = "\n"
)

func (o *ResultOutput) record(values ...interface{}) error {
	var data []byte
	for i, value := range values {
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		hasNext := i < len(values)-1
		data = append(data, b...)
		if hasNext {
			data = append(data, valueSeparator...)
		}
	}
	data = append(data, dataPointSeparator...)

	_, err := o.file.Write(data)
	return err
}
