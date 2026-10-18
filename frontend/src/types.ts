export interface Job {id:string;name:string;project:string;repositoryId?:string;type:string;command:string;args:string[];dir:string;timeoutSec:number}
export interface Repository {id:string;name:string;project:string;url:string;ref:string;revision:string;syncedAt:string;lastError:string;webhookEnabled:boolean;webhookLastDeliveryAt?:string;webhookLastStatus?:string;webhookLastRunId?:string;webhookLastError?:string;webhookTargetType:string;webhookTargetId:string}
export interface Workflow {id:string;name:string;project:string;jobIds:string[];requireApproval:boolean}
export interface Schedule {id:string;name:string;targetType:string;targetId:string;everyMinutes:number;enabled:boolean;nextAt:string}
export interface Run {id:string;targetType:string;targetId:string;targetName:string;status:string;step:number;log:string;startedAt:string;finishedAt?:string;triggeredBy:string;sources?:{jobId:string;repositoryId:string;commit:string}[]}
export interface Audit {at:string;action:string;detail:string}
export interface Workspace {jobs:Job[];repositories:Repository[];workflows:Workflow[];schedules:Schedule[];runs:Run[];audit:Audit[]}
export const emptyWorkspace:Workspace={jobs:[],repositories:[],workflows:[],schedules:[],runs:[],audit:[]};
