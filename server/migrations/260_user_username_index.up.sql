CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_user_username ON "user" (LOWER(username)) WHERE username IS NOT NULL AND username != '';
