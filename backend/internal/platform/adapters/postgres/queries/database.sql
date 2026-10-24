-- name: CurrentDatabaseName :one
SELECT current_database()::text;
