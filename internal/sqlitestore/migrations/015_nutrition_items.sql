-- Meals made of ingredients. An entry may list items, each with its own
-- optional amount and values for the portion used; the entry's own value
-- columns then hold the sums (written together with the items, in the same
-- transaction). Entries without items keep their directly entered values.
CREATE TABLE nutrition_items (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 entry_id INTEGER NOT NULL REFERENCES nutrition_entries(id) ON DELETE CASCADE,
 position INTEGER NOT NULL,
 name TEXT NOT NULL,
 amount_g REAL NULL CHECK (amount_g IS NULL OR amount_g > 0),
 calories_kcal REAL NULL CHECK (calories_kcal IS NULL OR calories_kcal >= 0),
 protein_g REAL NULL CHECK (protein_g IS NULL OR protein_g >= 0),
 carbs_g REAL NULL CHECK (carbs_g IS NULL OR carbs_g >= 0),
 fat_g REAL NULL CHECK (fat_g IS NULL OR fat_g >= 0),
 UNIQUE (entry_id, position)
);
CREATE INDEX nutrition_items_name ON nutrition_items(lower(name), id);
