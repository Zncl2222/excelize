package excelize

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPivotTableStyleTestFile(t *testing.T) *File {
	t.Helper()
	f := NewFile()
	t.Cleanup(func() { assert.NoError(t, f.Close()) })
	require.NoError(t, f.SetSheetRow("Sheet1", "A1", &[]interface{}{"Category", "Amount"}))
	require.NoError(t, f.SetSheetRow("Sheet1", "A2", &[]interface{}{"A", 10}))
	require.NoError(t, f.SetSheetRow("Sheet1", "A3", &[]interface{}{"B", 20}))
	require.NoError(t, f.SetSheetRow("Sheet1", "A4", &[]interface{}{"A", 30}))
	require.NoError(t, f.AddPivotTable(&PivotTableOptions{
		DataRange:       "Sheet1!A1:B4",
		PivotTableRange: "Sheet1!G4:M30",
		Name:            "PivotTable1",
		Rows:            []PivotTableField{{Data: "Category"}},
		Data:            []PivotTableField{{Data: "Amount", Subtotal: "Sum"}},
		RowGrandTotals:  true,
		ColGrandTotals:  true,
		ShowRowHeaders:  true,
		ShowColHeaders:  true,
	}))
	// Simulate a PivotTable result cached by a spreadsheet application. Newly
	// created Excelize PivotTables otherwise have no materialized sheet cells.
	require.NoError(t, f.SetCellValue("Sheet1", "G4", "cached pivot result"))
	return f
}

func TestSetPivotTableStyle(t *testing.T) {
	f := newPivotTableStyleTestFile(t)
	styleID, err := f.NewStyle(&Style{
		Border: []Border{{Type: "bottom", Color: "112233", Style: 1}},
		Fill:   Fill{Type: "pattern", Color: []string{"94D3A2"}, Pattern: 1},
		Font:   &Font{Bold: true, Color: "445566"},
		NumFmt: 2,
	})
	require.NoError(t, err)

	const pivotTableXML = "xl/pivotTables/pivotTable1.xml"
	part, ok := f.Pkg.Load(pivotTableXML)
	require.True(t, ok)
	pivotTableBefore := append([]byte(nil), part.([]byte)...)

	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "H5:J10", styleID))
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "H", styleID))
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "'sheet1'!I6", styleID))
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "G4:M30", styleID))

	ws, err := f.workSheetReader("Sheet1")
	require.NoError(t, err)
	require.Len(t, ws.ConditionalFormatting, 4)
	assert.Equal(t, []string{"H5:J10", "H4:H30", "I6", "G4:M30"}, []string{
		ws.ConditionalFormatting[0].SQRef,
		ws.ConditionalFormatting[1].SQRef,
		ws.ConditionalFormatting[2].SQRef,
		ws.ConditionalFormatting[3].SQRef,
	})
	for idx, conditionalFormatting := range ws.ConditionalFormatting {
		assert.True(t, conditionalFormatting.Pivot)
		require.Len(t, conditionalFormatting.CfRule, 1)
		rule := conditionalFormatting.CfRule[0]
		assert.Equal(t, "expression", rule.Type)
		assert.Equal(t, []string{"1"}, rule.Formula)
		require.NotNil(t, rule.DxfID)
		assert.Zero(t, *rule.DxfID)
		assert.Equal(t, len(ws.ConditionalFormatting)-idx, rule.Priority)
	}
	require.NotNil(t, f.Styles.Dxfs)
	assert.Equal(t, 1, f.Styles.Dxfs.Count)
	_, err = f.GetConditionalStyle(0)
	assert.NoError(t, err)
	currentStyle, err := f.GetCellStyle("Sheet1", "G4")
	require.NoError(t, err)
	assert.Equal(t, styleID, currentStyle)
	unusedStyle, err := f.GetCellStyle("Sheet1", "G30")
	require.NoError(t, err)
	assert.Zero(t, unusedStyle, "unused PivotTableRange cells must not be materialized")
	part, ok = f.Pkg.Load(pivotTableXML)
	require.True(t, ok)
	assert.Equal(t, pivotTableBefore, part.([]byte), "pivot table definition must not be changed")

	output := filepath.Join(t.TempDir(), "pivot-table-style.xlsx")
	require.NoError(t, f.SaveAs(output))
	reopened, err := OpenFile(output)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, reopened.Close()) })
	ws, err = reopened.workSheetReader("Sheet1")
	require.NoError(t, err)
	require.Len(t, ws.ConditionalFormatting, 4)
	for _, conditionalFormatting := range ws.ConditionalFormatting {
		assert.True(t, conditionalFormatting.Pivot)
		require.Len(t, conditionalFormatting.CfRule, 1)
		assert.Equal(t, []string{"1"}, conditionalFormatting.CfRule[0].Formula)
	}
}

func TestSetPivotTableStyleDoesNotMarkRegularConditionalFormatting(t *testing.T) {
	f := newPivotTableStyleTestFile(t)
	styleID, err := f.NewConditionalStyle(&Style{Font: &Font{Bold: true}})
	require.NoError(t, err)
	require.NoError(t, f.SetConditionalFormat("Sheet1", "A1:A4", []ConditionalFormatOptions{{
		Type:     "formula",
		Criteria: "TRUE",
		Format:   &styleID,
	}}))

	ws, err := f.workSheetReader("Sheet1")
	require.NoError(t, err)
	require.Len(t, ws.ConditionalFormatting, 1)
	assert.False(t, ws.ConditionalFormatting[0].Pivot)
}

func TestSetPivotTableStyleErrors(t *testing.T) {
	f := newPivotTableStyleTestFile(t)
	styleID, err := f.NewStyle(&Style{Font: &Font{Bold: true}})
	require.NoError(t, err)

	assert.EqualError(t, f.SetPivotTableStyle("SheetN", "PivotTable1", "H", styleID), "sheet SheetN does not exist")
	assert.Equal(t, ErrSheetNameInvalid, f.SetPivotTableStyle("Sheet:1", "PivotTable1", "H", styleID))
	assert.Equal(t, ErrParameterRequired, f.SetPivotTableStyle("Sheet1", "", "H", styleID))
	assert.EqualError(t, f.SetPivotTableStyle("Sheet1", "PivotTableN", "H", styleID), "table PivotTableN does not exist")
	assert.EqualError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "", styleID), "parameter 'RangeRef' parsing error: parameter is required")
	assert.EqualError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "Sheet2!H5", styleID), "parameter 'RangeRef' parsing error: worksheet does not match Sheet1")
	assert.EqualError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "A", styleID), "parameter 'RangeRef' parsing error: range must be within pivot table range G4:M30")
	assert.Error(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "H0", styleID))
	assert.Equal(t, newInvalidStyleID(-1), f.SetPivotTableStyle("Sheet1", "PivotTable1", "H", -1))
	assert.Equal(t, newInvalidStyleID(99), f.SetPivotTableStyle("Sheet1", "PivotTable1", "H", 99))
	require.NotNil(t, f.Styles.Dxfs)
	assert.Empty(t, f.Styles.Dxfs.Dxfs)
}

func TestSetPivotTableStyleReplacesRangeAndPreservesPrecedence(t *testing.T) {
	f := newPivotTableStyleTestFile(t)
	require.NoError(t, f.SetCellValue("Sheet1", "H5", "first range"))
	require.NoError(t, f.SetCellValue("Sheet1", "I6", "overlap"))
	redStyle, err := f.NewStyle(&Style{Fill: Fill{Type: "pattern", Color: []string{"FF0000"}, Pattern: 1}})
	require.NoError(t, err)
	blueStyle, err := f.NewStyle(&Style{Fill: Fill{Type: "pattern", Color: []string{"0000FF"}, Pattern: 1}})
	require.NoError(t, err)

	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "H5:J10", redStyle))
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "H5:J10", blueStyle))
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "I6:K12", redStyle))
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "I6:K12", redStyle))

	ws, err := f.workSheetReader("Sheet1")
	require.NoError(t, err)
	require.Len(t, ws.ConditionalFormatting, 2)
	assert.Equal(t, []string{"H5:J10", "I6:K12"}, []string{
		ws.ConditionalFormatting[0].SQRef,
		ws.ConditionalFormatting[1].SQRef,
	})
	assert.Equal(t, 2, ws.ConditionalFormatting[0].CfRule[0].Priority)
	assert.Equal(t, 1, ws.ConditionalFormatting[1].CfRule[0].Priority)
	require.NotNil(t, ws.ConditionalFormatting[0].CfRule[0].DxfID)
	require.NotNil(t, ws.ConditionalFormatting[1].CfRule[0].DxfID)
	assert.Equal(t, 1, *ws.ConditionalFormatting[0].CfRule[0].DxfID)
	assert.Zero(t, *ws.ConditionalFormatting[1].CfRule[0].DxfID)
	require.NotNil(t, f.Styles.Dxfs)
	assert.Equal(t, 2, f.Styles.Dxfs.Count)
	cellStyle, err := f.GetCellStyle("Sheet1", "H5")
	require.NoError(t, err)
	assert.Equal(t, blueStyle, cellStyle)
	cellStyle, err = f.GetCellStyle("Sheet1", "I6")
	require.NoError(t, err)
	assert.Equal(t, redStyle, cellStyle)
}

func TestSetPivotTableStyleDeduplicatesCustomNumberFormat(t *testing.T) {
	f := newPivotTableStyleTestFile(t)
	format := "0.000 units"
	styleID, err := f.NewStyle(&Style{CustomNumFmt: &format})
	require.NoError(t, err)
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "H5", styleID))
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "I6", styleID))
	require.NotNil(t, f.Styles.Dxfs)
	assert.Equal(t, 1, f.Styles.Dxfs.Count)
}

func TestSetPivotTableStyleReplacesRangeAfterReopen(t *testing.T) {
	f := newPivotTableStyleTestFile(t)
	styleID, err := f.NewStyle(&Style{Fill: Fill{Type: "pattern", Color: []string{"A1B2C3"}, Pattern: 1}})
	require.NoError(t, err)
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "H5:J10", styleID))
	output := filepath.Join(t.TempDir(), "pivot-table-style-replace.xlsx")
	require.NoError(t, f.SaveAs(output))
	reopened, err := OpenFile(output)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, reopened.Close()) })

	require.NoError(t, reopened.SetPivotTableStyle("Sheet1", "PivotTable1", "H5:J10", styleID))
	ws, err := reopened.workSheetReader("Sheet1")
	require.NoError(t, err)
	require.Len(t, ws.ConditionalFormatting, 1)
	require.NotNil(t, reopened.Styles.Dxfs)
	assert.Equal(t, 1, reopened.Styles.Dxfs.Count)
}

func TestSetPivotTableStyleSelectsPivotTableByName(t *testing.T) {
	f := newPivotTableStyleTestFile(t)
	require.NoError(t, f.AddPivotTable(&PivotTableOptions{
		DataRange:       "Sheet1!A1:B4",
		PivotTableRange: "Sheet1!N4:T30",
		Name:            "PivotTable2",
		Rows:            []PivotTableField{{Data: "Category"}},
		Data:            []PivotTableField{{Data: "Amount", Subtotal: "Sum"}},
	}))
	styleID, err := f.NewStyle(&Style{Font: &Font{Bold: true}})
	require.NoError(t, err)
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable2", "O5", styleID))
	assert.Error(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "O5", styleID))

	ws, err := f.workSheetReader("Sheet1")
	require.NoError(t, err)
	require.Len(t, ws.ConditionalFormatting, 1)
	assert.Equal(t, "O5", ws.ConditionalFormatting[0].SQRef)
}

func TestSetPivotTableStyleX14Priority(t *testing.T) {
	f := newPivotTableStyleTestFile(t)
	require.NoError(t, f.SetConditionalFormat("Sheet1", "A1:A4", []ConditionalFormatOptions{{
		Type: "icon_set", IconStyle: "3Stars",
	}}))
	styleID, err := f.NewStyle(&Style{Font: &Font{Bold: true}})
	require.NoError(t, err)
	require.NoError(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "H5", styleID))
	require.NoError(t, f.SetConditionalFormat("Sheet1", "B1:B4", []ConditionalFormatOptions{{
		Type: "icon_set", IconStyle: "5Boxes",
	}}))

	ws, err := f.workSheetReader("Sheet1")
	require.NoError(t, err)
	require.Len(t, ws.ConditionalFormatting, 1)
	assert.Equal(t, 1, ws.ConditionalFormatting[0].CfRule[0].Priority)
	priorities, _ := getX14ConditionalFormatPriorities(ws)
	assert.Equal(t, []int{2, 3}, priorities)
}

func TestSetPivotTableStyleConcurrent(t *testing.T) {
	f := newPivotTableStyleTestFile(t)
	require.NoError(t, f.SetCellValue("Sheet1", "H5", "concurrent"))
	redStyle, err := f.NewStyle(&Style{Fill: Fill{Type: "pattern", Color: []string{"FF0000"}, Pattern: 1}})
	require.NoError(t, err)
	blueStyle, err := f.NewStyle(&Style{Fill: Fill{Type: "pattern", Color: []string{"0000FF"}, Pattern: 1}})
	require.NoError(t, err)

	const workers = 8
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			styleID := redStyle
			if worker%2 == 1 {
				styleID = blueStyle
			}
			errs <- f.SetPivotTableStyle("Sheet1", "PivotTable1", "H5", styleID)
		}(worker)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	ws, err := f.workSheetReader("Sheet1")
	require.NoError(t, err)
	require.Len(t, ws.ConditionalFormatting, 1)
	rule := ws.ConditionalFormatting[0].CfRule[0]
	assert.Equal(t, 1, rule.Priority)
	require.NotNil(t, rule.DxfID)
	cellStyleID, err := f.GetCellStyle("Sheet1", "H5")
	require.NoError(t, err)
	cellStyle, err := f.GetStyle(cellStyleID)
	require.NoError(t, err)
	conditionalStyle, err := f.GetConditionalStyle(*rule.DxfID)
	require.NoError(t, err)
	assert.Equal(t, cellStyle.Fill, conditionalStyle.Fill)
}

func TestSetPivotTableStyleIsAtomicOnInvalidMaterializedCell(t *testing.T) {
	f := newPivotTableStyleTestFile(t)
	styleID, err := f.NewStyle(&Style{Font: &Font{Bold: true}})
	require.NoError(t, err)
	ws, err := f.workSheetReader("Sheet1")
	require.NoError(t, err)
	require.NotEmpty(t, ws.SheetData.Row[3].C)
	ws.SheetData.Row[3].C[0].R = "invalid"

	assert.Error(t, f.SetPivotTableStyle("Sheet1", "PivotTable1", "G4", styleID))
	assert.Empty(t, ws.ConditionalFormatting)
	if f.Styles.Dxfs != nil {
		assert.Empty(t, f.Styles.Dxfs.Dxfs)
	}
}
