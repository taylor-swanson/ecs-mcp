-- Main table storing all ECS fields.
CREATE TABLE IF NOT EXISTS fields (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    dashed_name TEXT NOT NULL UNIQUE,
    type TEXT NOT NULL,
    level TEXT NOT NULL,
    short TEXT,
    description TEXT,
    example TEXT
);
