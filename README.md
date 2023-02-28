## Lab 2 - Raft, Go Version

This file details the contents of the initial Lab 2 code repository and how to use it.

### Getting started

If you're using the same programming language for this lab as the previous one, the look and feel of this
lab should look familiar, and your environment shouldn't need any changes.

### Initial repository contents

The top-level directory (called `lab2-go` here) of the initial starter-code repository includes four things:
* This `README.md` file
* The Lab 2 `Makefile`, described in detail later
* The `src` source directory, containing both
    * The `remote` library directory, where you should copy your `remote` package from lab 1
    * The `raft` source directory, which is where all your work will be done, along-side the test code.

Visually, this looks roughly like the following:
```
\---lab2-go
    +---src
        +---remote
        +---raft
        |   +---raft.go
        |   \---test_test.go
    +---Makefile
    +---README.md
    \---run_autograder
```
The details of each of these will hopefully become clear after reading the rest of this file.


### Implementing Raft

The first things you'll do in this lab is to copy your `remote` library code from Lab 1 into the corresponding
directory in your Lab 2 repository.  It is ok if you need to make changes to your `remote` library for Lab 2.  You
do not need to copy the test code from lab 1, just the `.go` files comprising the library itself.

The `raft` package initially includes a rough outline of what you are required to implement, mainly based on the
Raft paper but also adhering to the needs of our test suite (see below).  Your primary task is to complete the
implementation of the Raft protocol according to the specifications given in the Canvas assignment.  You are free
to create additional source files within the `raft` package as needed, and you can use whatever data structures, 
functions, and go routines that you desire to implement the protocol.  However, you cannot change the test suite
(or at least, your code must work with an unmodified test suite), so the provided interactions between `raft.go`
and `test_test.go` must be maintained.  You are welcome (and encouraged) to read through the test code to see how
the test Controller and the test cases work and what they are testing.


### Testing your Raft Implementation

Once you're at the point where you want to run any of the provided tests, you can either use the appropriate 
`go test` commands or the provided `make` rules `checkpoint` and `test` as used in the previous labs.  The `Makefile` 
also includes rules that run Go's race detector to ensure that your implementation is thread safe.  These rules are 
`checkpoint-race` and `test-race`, and these rules are used in the auto-grader.

As with previous labs, you are certainly welcome to create your own test and supplementary evaluation code.  You are 
also welcome to create additional `make` rules in the Makefile, but we ask that you keep the existing rules, as we will 
use them for lab grading.  We've also included a copy of our grading script `run_autograder`, which you can run to see 
how the autograder uses `make` and how your scores will be computed.  Remember to toggle the comments to run 
`checkpoint`/`checkpoint-race` or `test`/`test-race` as desired.


### Generating documentation

We want you to get in the habit of documenting your code in a way that leads to detailed, easy-to-read/-navigate 
package documentation for your code package. Our Makefile includes a `docs` rule that will pipe the output of the 
`go doc` command into a text file that is reasonably easy to read.  We will use this output for the manually graded 
parts of the lab, so good comments are valuable.


### Questions?

If there is any part of the initial repository, environment setup, lab requirements, or anything else, please do not 
hesitate to ask.  We're here to help!

