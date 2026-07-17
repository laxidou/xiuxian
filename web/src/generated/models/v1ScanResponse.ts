/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { v1OpportunitySignal } from './v1OpportunitySignal';
import type { v1ScanRole } from './v1ScanRole';
export type v1ScanResponse = {
    roles?: Array<v1ScanRole>;
    opportunities?: Array<v1OpportunitySignal>;
    hasMore?: boolean;
};

