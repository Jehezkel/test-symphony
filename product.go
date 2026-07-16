package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var errProductNotFound = errors.New("product not found")

type product struct {
	ID    int64
	Name  string
	Price string
	EAN   string
}

type productStore struct{ db *sql.DB }

func newProductStore(db *sql.DB) *productStore { return &productStore{db: db} }

func (s *productStore) list(ctx context.Context, userID int64) ([]product, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, current_price_minor, ean FROM products WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()
	var items []product
	for rows.Next() {
		var item product
		var price int64
		if err := rows.Scan(&item.ID, &item.Name, &price, &item.EAN); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		item.Price = formatMinorUnits(price)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *productStore) create(ctx context.Context, userID int64, name string, price int64, ean string) (product, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO products(user_id, sku, name, ean, current_price_minor) VALUES (?, ?, ?, ?, ?)`, userID, ean, name, ean, price)
	if err != nil {
		return product{}, fmt.Errorf("create product: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return product{}, fmt.Errorf("product id: %w", err)
	}
	return product{ID: id, Name: name, Price: formatMinorUnits(price), EAN: ean}, nil
}

func (s *productStore) get(ctx context.Context, userID, id int64) (product, error) {
	var item product
	var price int64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, current_price_minor, ean FROM products WHERE id = ? AND user_id = ?`, id, userID).Scan(&item.ID, &item.Name, &price, &item.EAN)
	if errors.Is(err, sql.ErrNoRows) {
		return product{}, errProductNotFound
	}
	if err != nil {
		return product{}, fmt.Errorf("get product: %w", err)
	}
	item.Price = formatMinorUnits(price)
	return item, nil
}

func (s *productStore) update(ctx context.Context, userID, id int64, name string, price int64, ean string) (product, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE products SET name = ?, ean = ?, sku = ?, current_price_minor = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`, name, ean, ean, price, id, userID)
	if err != nil {
		return product{}, fmt.Errorf("update product: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return product{}, fmt.Errorf("updated rows: %w", err)
	}
	if count == 0 {
		return product{}, errProductNotFound
	}
	return product{ID: id, Name: name, Price: formatMinorUnits(price), EAN: ean}, nil
}

func (s *productStore) delete(ctx context.Context, userID, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM products WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete product: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("deleted rows: %w", err)
	}
	if count == 0 {
		return errProductNotFound
	}
	return nil
}
