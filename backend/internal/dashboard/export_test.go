package dashboard

import "github.com/kerti/afloat/backend/internal/api"

// The wire conversions are unexported (the handler is their one caller); the
// external tests read them through these.
func (b Breakdown) CategoriesOnTheWireForTest() *[]api.CategoryFigure { return b.categoriesOnTheWire() }
func (b Breakdown) PocketsOnTheWireForTest() *[]api.PocketFigure      { return b.pocketsOnTheWire() }

func (c Commitments) OnTheWireForTest() *api.Commitments { return c.onTheWire() }
