/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { v1Position } from './v1Position';
export type v1MoveRequest = {
    idempotencyKey?: string;
    target?: v1Position;
    expectedLifeNumber?: string;
    expectedStateVersion?: string;
};

