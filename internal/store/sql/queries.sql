-- name: InsertField :one
INSERT INTO
    fields (name, dashed_name, type, level, short, description, example)
    VALUES (?, ?, ?, ?, ?, ?, ?)
    RETURNING id;
