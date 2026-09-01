-- name: DatabaseSize :one
-- How much disk this database occupies, tables, indexes and toast together.
--
-- Asked of the current database rather than the whole cluster: srosha is the
-- only thing in it, and pg_database_size on a name would need one.
SELECT pg_database_size(current_database())::bigint AS bytes;
