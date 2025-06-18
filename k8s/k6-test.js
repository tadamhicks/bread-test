import http from 'k6/http';
import { check, sleep } from 'k6';

export let options = {
  scenarios: {
    constant_load: {
      executor: 'constant-arrival-rate',
      rate: 2,              // 2 iterations per second = ~120 per minute
      timeUnit: '1s',       // 1 second
      duration: '1m',       // Run for 1 minute
      preAllocatedVUs: 5,   // Initial pool of VUs
      maxVUs: 10,          // Maximum pool of VUs
    },
  },
};

const BASE_URL = 'http://bookapi.books.svc.cluster.local';

// Sample book data
const testBook = {
  title: 'Test Book',
  author: 'Test Author',
  year: 2025
};

export default function() {
  const rand = Math.random();
  
  // GET all books (25% of requests)
  if (rand < 0.25) {
    const getAll = http.get(`${BASE_URL}/books`);
    check(getAll, {
      'get all books status is 200': (r) => r.status === 200,
    });
  }
  
  // GET book by ID (25% of requests)
  else if (rand < 0.5) {
    const getAll = http.get(`${BASE_URL}/books`);
    if (getAll.status === 200) {
      const books = JSON.parse(getAll.body);
      if (books.length > 0) {
        const randomBook = books[Math.floor(Math.random() * books.length)];
        const getOne = http.get(`${BASE_URL}/books?id=${randomBook.id}`);
        check(getOne, {
          'get book by id status is 200': (r) => r.status === 200,
        });
      }
    }
  }
  
  // POST new book (20% of requests)
  else if (rand < 0.7) {
    const create = http.post(`${BASE_URL}/books`, JSON.stringify(testBook), {
      headers: { 'Content-Type': 'application/json' },
    });
    check(create, {
      'create book status is 201': (r) => r.status === 201,
    });
  }
  
  // PUT update book (15% of requests)
  else if (rand < 0.85) {
    const getAll = http.get(`${BASE_URL}/books`);
    if (getAll.status === 200) {
      const books = JSON.parse(getAll.body);
      if (books.length > 0) {
        const randomBook = books[Math.floor(Math.random() * books.length)];
        const updatedBook = {
          ...testBook,
          title: `Updated Book ${Date.now()}`,
        };
        const update = http.put(`${BASE_URL}/books?id=${randomBook.id}`, JSON.stringify(updatedBook), {
          headers: { 'Content-Type': 'application/json' },
        });
        check(update, {
          'update book status is 200': (r) => r.status === 200,
        });
      }
    }
  }
  
  // DELETE book (15% of requests)
  else {
    const getAll = http.get(`${BASE_URL}/books`);
    if (getAll.status === 200) {
      const books = JSON.parse(getAll.body);
      if (books.length > 0) {
        const randomBook = books[Math.floor(Math.random() * books.length)];
        const del = http.del(`${BASE_URL}/books?id=${randomBook.id}`);
        check(del, {
          'delete book status is 204': (r) => r.status === 204,
        });
      }
    }
  }
  
  // Small random delay (0-100ms) to prevent exact simultaneous requests
  sleep(Math.random() * 0.1);
}
