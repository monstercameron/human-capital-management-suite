package productui

import "fmt"

func latencyPeople(count int) []Person {
	people := make([]Person, count)
	for index := range people {
		people[index] = Person{
			ID: fmt.Sprintf("id-%09d", index), Name: fmt.Sprintf("Person %09d", count-index),
			Role: fmt.Sprintf("Role %03d", index%200), Team: fmt.Sprintf("Team %03d", index%75),
			Manager: fmt.Sprintf("Manager %04d", index%1000), Location: fmt.Sprintf("Location %03d", index%120),
		}
	}
	return people
}

func latencyDataTable(rowCount, columnCount int) DataTableProps {
	columns := make([]DataTableColumnProps, columnCount)
	for column := range columns {
		columns[column] = DataTableColumnProps{ID: fmt.Sprintf("c%d", column), Label: fmt.Sprintf("Column %d", column)}
	}
	rows := make([]DataTableRowProps, rowCount)
	for row := range rows {
		cells := make([]DataTableCellProps, columnCount)
		for column := range cells {
			cells[column] = DataTableCellProps{ColumnID: columns[column].ID, Text: "value"}
		}
		rows[row] = DataTableRowProps{ID: fmt.Sprint(row), Cells: cells}
	}
	return DataTableProps{Caption: "Latency gate matrix", Columns: columns, Rows: rows}
}
