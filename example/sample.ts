// examples/sample.ts

// @ts-ignore
import * as fs from 'fs';
// @ts-ignore
import { join } from 'path';

function greet(name: string): void {
    console.log(`Hello, ${name}!`);
}

class Person {
    constructor(private name: string) {}

    sayHi(): void {
        greet(this.name);
    }
}

// This arrow function is assigned to a variable
const arrowFunc = (msg: string): void => {
    console.log(msg);
};

function main() {
    const p = new Person("Connor");
    p.sayHi();
    arrowFunc("Goodbye!");
}

main();
