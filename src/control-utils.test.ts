import {expect,it} from 'vitest';
import {makeTaskID} from './control-utils';
it('creates valid distinct task keys on a LAN without randomUUID',()=>{
 const original=crypto.randomUUID;
 Object.defineProperty(crypto,'randomUUID',{value:undefined,configurable:true});
 try {const a=makeTaskID(),b=makeTaskID();expect(a).toMatch(/^[a-f0-9]{32}$/);expect(a).not.toBe(b)}
 finally {Object.defineProperty(crypto,'randomUUID',{value:original,configurable:true})}
});
