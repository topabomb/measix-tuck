// Test-data generation only. Authentication is implemented by Go.
import {randomBytes,scrypt} from 'node:crypto';
import {promisify} from 'node:util';
const derive=promisify(scrypt);
export async function passwordHash(password,minLength=12){if(password.length<minLength||Buffer.byteLength(password)>1024)throw new Error('Invalid test password');const salt=randomBytes(16).toString('hex');const key=await derive(password,salt,32,{N:32768,r:8,p:3,maxmem:64*1024*1024});return `scrypt$32768$8$3$${salt}$${key.toString('hex')}`;}
