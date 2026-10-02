BEGIN;

-- Pre-joined game card projection shared by all card-returning queries.
-- developer: first developer (role='developer') alphabetically.
-- media_key: first header by position (460x215 fits cards best), else first
-- capsule by position. Use view.MediaURL(key) to resolve.
CREATE VIEW game_cards AS
SELECT g.id,
       g.slug,
       g.title,
       g.tagline,
       g.release_date,
       g.release_status,
       g.gem_score,
       pd.name  AS developer_name,
       pd.slug  AS developer_slug,
       m.key    AS media_key
FROM games g
LEFT JOIN LATERAL (
    SELECT d.name, d.slug
    FROM game_developers gd
    JOIN developers d ON d.id = gd.developer_id
    WHERE gd.game_id = g.id AND gd.role = 'developer'
    ORDER BY d.name LIMIT 1
) pd ON true
LEFT JOIN LATERAL (
    SELECT m2.key FROM media m2
    WHERE m2.game_id = g.id AND m2.kind IN ('header', 'capsule')
    ORDER BY m2.kind = 'capsule', m2.position LIMIT 1
) m ON true;

COMMIT;
