/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { v1Direction } from './v1Direction';
export type v1MoveDirectionRequest = {
    idempotencyKey?: string;
    direction?: v1Direction;
    speed?: string;
    expectedLifeNumber?: string;
    expectedStateVersion?: string;
};

