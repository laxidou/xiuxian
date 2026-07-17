/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { v1GameRuleSection } from './v1GameRuleSection';
import type { v1RealmRule } from './v1RealmRule';
export type v1GameRules = {
    ruleVersion?: number;
    title?: string;
    summary?: string;
    sections?: Array<v1GameRuleSection>;
    realms?: Array<v1RealmRule>;
    aiRules?: string;
    canonicalUrl?: string;
};

