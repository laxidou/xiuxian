-- +goose Up
ALTER TABLE trajectories
    ADD COLUMN mode text NOT NULL DEFAULT 'target',
    ADD COLUMN direction text,
    ADD COLUMN desired_speed bigint,
    ADD COLUMN cultivation_basis bigint NOT NULL DEFAULT 0,
    ALTER COLUMN target_x DROP NOT NULL,
    ALTER COLUMN target_y DROP NOT NULL;

ALTER TABLE trajectories
    ADD CONSTRAINT trajectories_mode_check CHECK (mode IN ('target', 'direction')),
    ADD CONSTRAINT trajectories_direction_check CHECK (direction IS NULL OR direction IN ('up', 'down', 'left', 'right')),
    ADD CONSTRAINT trajectories_shape_check CHECK (
        (mode = 'target' AND target_x IS NOT NULL AND target_y IS NOT NULL AND direction IS NULL AND desired_speed IS NULL)
        OR
        (mode = 'direction' AND target_x IS NULL AND target_y IS NULL AND direction IS NOT NULL AND desired_speed > 0)
    );

-- +goose Down
DELETE FROM trajectories WHERE mode = 'direction';
ALTER TABLE trajectories
    DROP CONSTRAINT trajectories_shape_check,
    DROP CONSTRAINT trajectories_direction_check,
    DROP CONSTRAINT trajectories_mode_check,
    ALTER COLUMN target_x SET NOT NULL,
    ALTER COLUMN target_y SET NOT NULL,
    DROP COLUMN cultivation_basis,
    DROP COLUMN desired_speed,
    DROP COLUMN direction,
    DROP COLUMN mode;
