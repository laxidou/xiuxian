/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { v1Position } from './v1Position';
export type v1ReincarnateRequest = {
    idempotencyKey?: string;
    position?: v1Position;
    random?: boolean;
    expectedLifeNumber?: string;
    expectedStateVersion?: string;
};

