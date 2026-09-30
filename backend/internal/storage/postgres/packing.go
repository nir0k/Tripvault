package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nir0k/tripvault/backend/internal/domain"
)

// PackingRepository reads and writes the packing list of a trip.
//
// Every change that moves a position locks the trip's row first, so two people
// adding to one list at the same moment get positions one after the other.
type PackingRepository struct {
	pool *pgxpool.Pool
}

// NewPackingRepository - creates the packing list repository.
//
// Arguments:
//   - pool: an established connection pool.
//
// Returns:
//   - a repository bound to that pool.
func NewPackingRepository(pool *pgxpool.Pool) *PackingRepository {
	return &PackingRepository{pool: pool}
}

const packingCategoryColumns = `c.id, c.trip_id, c.name, c.color, c.icon, c.position`

const packingItemColumns = `i.id, i.trip_id, i.category_id, i.name, i.quantity, i.note, i.packed, i.bringer_id,
	i.position, i.created_at, i.updated_at`

// scanPackingCategory reads one row in the order of packingCategoryColumns.
func scanPackingCategory(row pgx.Row) (domain.PackingCategory, error) {
	var c domain.PackingCategory
	err := row.Scan(&c.ID, &c.TripID, &c.Name, &c.Color, &c.Icon, &c.Position)
	return c, err
}

// scanPackingItem reads one row in the order of packingItemColumns.
func scanPackingItem(row pgx.Row) (domain.PackingItem, error) {
	var i domain.PackingItem
	err := row.Scan(&i.ID, &i.TripID, &i.CategoryID, &i.Name, &i.Quantity, &i.Note, &i.Packed, &i.BringerID,
		&i.Position, &i.CreatedAt, &i.UpdatedAt)
	return i, err
}

// List - reads a trip's whole packing list.
//
// Arguments:
//   - ctx: context bounding the queries.
//   - tripID: the trip.
//
// Returns:
//   - the categories in their order and the items ordered within their category.
//   - an error if a query fails.
func (r *PackingRepository) List(ctx context.Context, tripID uuid.UUID) (domain.PackingList, error) {
	return r.listIn(ctx, r.pool, tripID)
}

// listIn reads a trip's whole list through q, the pool or a transaction.
func (r *PackingRepository) listIn(ctx context.Context, q querier, tripID uuid.UUID) (domain.PackingList, error) {
	rows, err := q.Query(ctx, `SELECT `+packingCategoryColumns+` FROM packing_categories c
		WHERE c.trip_id = $1 ORDER BY c.position, c.id`, tripID)
	if err != nil {
		return domain.PackingList{}, fmt.Errorf("list packing categories: %w", err)
	}
	categories, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PackingCategory, error) {
		return scanPackingCategory(row)
	})
	if err != nil {
		return domain.PackingList{}, fmt.Errorf("read packing categories: %w", err)
	}
	rows, err = q.Query(ctx, `SELECT `+packingItemColumns+` FROM packing_items i
		WHERE i.trip_id = $1 ORDER BY i.position, i.created_at, i.id`, tripID)
	if err != nil {
		return domain.PackingList{}, fmt.Errorf("list packing items: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PackingItem, error) {
		return scanPackingItem(row)
	})
	if err != nil {
		return domain.PackingList{}, fmt.Errorf("read packing items: %w", err)
	}
	return domain.PackingList{Categories: categories, Items: items}, nil
}

// Category - reads one category of a packing list.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the category.
//
// Returns:
//   - the category.
//   - domain.ErrNotFound when it does not exist.
func (r *PackingRepository) Category(ctx context.Context, id uuid.UUID) (domain.PackingCategory, error) {
	return oneRow(scanPackingCategory, r.pool.QueryRow(ctx,
		`SELECT `+packingCategoryColumns+` FROM packing_categories c WHERE c.id = $1`, id), "get packing category")
}

// Item - reads one item of a packing list.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the item.
//
// Returns:
//   - the item.
//   - domain.ErrNotFound when it does not exist.
func (r *PackingRepository) Item(ctx context.Context, id uuid.UUID) (domain.PackingItem, error) {
	return oneRow(scanPackingItem, r.pool.QueryRow(ctx,
		`SELECT `+packingItemColumns+` FROM packing_items i WHERE i.id = $1`, id), "get packing item")
}

// checkPackingCategory refuses a category that is not part of the trip's list.
// A nil category means the items without one.
func checkPackingCategory(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, categoryID *uuid.UUID) error {
	if categoryID == nil {
		return nil
	}
	var owner uuid.UUID
	err := tx.QueryRow(ctx, `SELECT trip_id FROM packing_categories WHERE id = $1`, *categoryID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != tripID) {
		return domain.NewValidationError("category_id", "unknown_category", "the category is not part of this list")
	}
	if err != nil {
		return fmt.Errorf("find packing category: %w", err)
	}
	return nil
}

// renumberPackingItems numbers one category's items from zero, leaving except
// out, and returns how many there are without it.
func renumberPackingItems(ctx context.Context, tx pgx.Tx, tripID uuid.UUID, categoryID *uuid.UUID,
	except uuid.UUID) (int, error) {
	_, err := tx.Exec(ctx,
		`UPDATE packing_items i SET position = r.rn - 1
		 FROM (SELECT id, row_number() OVER (ORDER BY position, created_at, id) AS rn FROM packing_items
		       WHERE trip_id = $1 AND category_id IS NOT DISTINCT FROM $2 AND id <> $3) r
		 WHERE i.id = r.id AND i.position <> r.rn - 1`, tripID, categoryID, except)
	if err != nil {
		return 0, fmt.Errorf("number packing items: %w", err)
	}
	var count int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM packing_items WHERE trip_id = $1 AND category_id IS NOT DISTINCT FROM $2 AND id <> $3`,
		tripID, categoryID, except).Scan(&count); err != nil {
		return 0, fmt.Errorf("count packing items: %w", err)
	}
	return count, nil
}

// CreateCategory - adds a category at the end of a trip's list, in the colour its
// place picks when it names none.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - category: the validated category with its identifier and trip set.
//
// Returns:
//   - domain.ErrNotFound when the trip does not exist.
func (r *PackingRepository) CreateCategory(ctx context.Context, category domain.PackingCategory) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockTrip(ctx, tx, category.TripID); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM packing_categories WHERE trip_id = $1`,
			category.TripID).Scan(&count); err != nil {
			return fmt.Errorf("count packing categories: %w", err)
		}
		if category.Color == "" {
			category.Color = domain.DefaultPackingColor(count)
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO packing_categories (id, trip_id, name, color, icon, position) VALUES ($1, $2, $3, $4, $5, $6)`,
			category.ID, category.TripID, category.Name, category.Color, category.Icon, count)
		if err != nil {
			return fmt.Errorf("create packing category: %w", err)
		}
		return nil
	})
}

// AddSections - adds categories with their items to a trip's list at once,
// filling a category the list has under the same name and leaving out the
// things it holds already (domain.PackingList.Merge).
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tripID: the trip.
//   - sections: the validated sections, with the identifiers new rows take.
//
// Returns:
//   - domain.ErrNotFound when the trip does not exist.
func (r *PackingRepository) AddSections(ctx context.Context, tripID uuid.UUID,
	sections []domain.PackingSection) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockTrip(ctx, tx, tripID); err != nil {
			return err
		}
		// The list is read inside the transaction, after the lock, so the
		// positions worked out from it stay free until it commits.
		list, err := r.listIn(ctx, tx, tripID)
		if err != nil {
			return err
		}
		categories, items := list.Merge(sections)
		for _, category := range categories {
			if _, err := tx.Exec(ctx,
				`INSERT INTO packing_categories (id, trip_id, name, color, icon, position) VALUES ($1, $2, $3, $4, $5, $6)`,
				category.ID, tripID, category.Name, category.Color, category.Icon, category.Position); err != nil {
				return fmt.Errorf("create packing category: %w", err)
			}
		}
		for _, item := range items {
			if _, err := tx.Exec(ctx,
				`INSERT INTO packing_items (id, trip_id, category_id, name, quantity, note, position)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				item.ID, tripID, item.CategoryID, item.Name, item.Quantity, item.Note, item.Position); err != nil {
				return fmt.Errorf("create packing item: %w", err)
			}
		}
		return nil
	})
}

// UpdateCategory - stores a category's name, colour and icon.
//
// Arguments:
//   - ctx: context bounding the query.
//   - category: the validated category, with its colour set.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *PackingRepository) UpdateCategory(ctx context.Context, category domain.PackingCategory) error {
	tag, err := r.pool.Exec(ctx, `UPDATE packing_categories SET name = $2, color = $3, icon = $4 WHERE id = $1`,
		category.ID, category.Name, category.Color, category.Icon)
	if err != nil {
		return fmt.Errorf("update packing category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteCategory - removes a category with every item in it and closes the gap
// it leaves.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - category: the category.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *PackingRepository) DeleteCategory(ctx context.Context, category domain.PackingCategory) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockTrip(ctx, tx, category.TripID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM packing_categories WHERE id = $1`, category.ID)
		if err != nil {
			return fmt.Errorf("delete packing category: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		if _, err := tx.Exec(ctx,
			`UPDATE packing_categories c SET position = r.rn - 1
			 FROM (SELECT id, row_number() OVER (ORDER BY position, id) AS rn FROM packing_categories
			       WHERE trip_id = $1) r
			 WHERE c.id = r.id AND c.position <> r.rn - 1`, category.TripID); err != nil {
			return fmt.Errorf("number packing categories: %w", err)
		}
		return nil
	})
}

// ReorderCategories - puts a trip's categories in a new order.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - tripID: the trip.
//   - order: every category of the trip, each once, in the new order.
//
// Returns:
//   - a *domain.ValidationError when the order does not name exactly the
//     trip's categories.
func (r *PackingRepository) ReorderCategories(ctx context.Context, tripID uuid.UUID, order []uuid.UUID) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockTrip(ctx, tx, tripID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			`UPDATE packing_categories c SET position = o.n - 1
			 FROM unnest($2::uuid[]) WITH ORDINALITY AS o (id, n)
			 WHERE c.id = o.id AND c.trip_id = $1`, tripID, order)
		if err != nil {
			return fmt.Errorf("reorder packing categories: %w", err)
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM packing_categories WHERE trip_id = $1`, tripID).
			Scan(&count); err != nil {
			return fmt.Errorf("count packing categories: %w", err)
		}
		if int(tag.RowsAffected()) != len(order) || count != len(order) {
			return domain.NewValidationError("order", "incomplete", "must name every category of the list once")
		}
		return nil
	})
}

// CreateItem - adds an item at the end of its category.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - item: the validated item with its identifier and trip set.
//
// Returns:
//   - domain.ErrNotFound when the trip does not exist.
//   - a *domain.ValidationError when the category is not part of the trip's list.
func (r *PackingRepository) CreateItem(ctx context.Context, item domain.PackingItem) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockTrip(ctx, tx, item.TripID); err != nil {
			return err
		}
		if err := checkPackingCategory(ctx, tx, item.TripID, item.CategoryID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO packing_items (id, trip_id, category_id, name, quantity, note, packed, bringer_id, position)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
			         (SELECT coalesce(max(position) + 1, 0) FROM packing_items
			          WHERE trip_id = $2 AND category_id IS NOT DISTINCT FROM $3))`,
			item.ID, item.TripID, item.CategoryID, item.Name, item.Quantity, item.Note, item.Packed, item.BringerID)
		if err != nil {
			return fmt.Errorf("create packing item: %w", err)
		}
		return nil
	})
}

// UpdateItem - stores an item's own fields; its category and position change
// only by MoveItem.
//
// Arguments:
//   - ctx: context bounding the query.
//   - item: the validated item.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *PackingRepository) UpdateItem(ctx context.Context, item domain.PackingItem) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE packing_items SET name = $2, quantity = $3, note = $4, packed = $5, bringer_id = $6,
		                          updated_at = now()
		 WHERE id = $1`, item.ID, item.Name, item.Quantity, item.Note, item.Packed, item.BringerID)
	if err != nil {
		return fmt.Errorf("update packing item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// MoveItem - moves an item to a position of a category, its own or another.
//
// Arguments:
//   - ctx: context bounding the transaction.
//   - item: the item as stored.
//   - categoryID: the category to move it to, or nil for the items without one.
//   - position: its place in that category; out of range means the end.
//
// Returns:
//   - domain.ErrNotFound when the item no longer exists.
//   - a *domain.ValidationError when the category is not part of the trip's list.
func (r *PackingRepository) MoveItem(ctx context.Context, item domain.PackingItem, categoryID *uuid.UUID,
	position int) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockTrip(ctx, tx, item.TripID); err != nil {
			return err
		}
		if err := checkPackingCategory(ctx, tx, item.TripID, categoryID); err != nil {
			return err
		}
		count, err := renumberPackingItems(ctx, tx, item.TripID, categoryID, item.ID)
		if err != nil {
			return err
		}
		if position < 0 || position > count {
			position = count
		}
		if _, err := tx.Exec(ctx,
			`UPDATE packing_items SET position = position + 1
			 WHERE trip_id = $1 AND category_id IS NOT DISTINCT FROM $2 AND id <> $3 AND position >= $4`,
			item.TripID, categoryID, item.ID, position); err != nil {
			return fmt.Errorf("open a slot: %w", err)
		}
		tag, err := tx.Exec(ctx,
			`UPDATE packing_items SET category_id = $2, position = $3, updated_at = now() WHERE id = $1`,
			item.ID, categoryID, position)
		if err != nil {
			return fmt.Errorf("move packing item: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}
		// The category the item left is numbered afresh, so its gap closes.
		if !sameCategory(item.CategoryID, categoryID) {
			if _, err := renumberPackingItems(ctx, tx, item.TripID, item.CategoryID, uuid.Nil); err != nil {
				return err
			}
		}
		return nil
	})
}

// sameCategory compares two optional category identifiers.
func sameCategory(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// DeleteItem - removes an item.
//
// Arguments:
//   - ctx: context bounding the query.
//   - id: the item.
//
// Returns:
//   - domain.ErrNotFound when it no longer exists.
func (r *PackingRepository) DeleteItem(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM packing_items WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete packing item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ResetPacked - takes the tick off every item of a trip's list, for packing it
// again: the way back, or the next trip.
//
// Arguments:
//   - ctx: context bounding the query.
//   - tripID: the trip.
//
// Returns:
//   - an error if the query fails.
func (r *PackingRepository) ResetPacked(ctx context.Context, tripID uuid.UUID) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE packing_items SET packed = false, updated_at = now() WHERE trip_id = $1 AND packed`,
		tripID); err != nil {
		return fmt.Errorf("reset packing list: %w", err)
	}
	return nil
}
