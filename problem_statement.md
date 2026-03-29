Part 1: Choose a general title and frame a problem statement around it:

Project Titles
(1) Cloud Deployment Choices Impacting Performance of a Web Application Using Microservices Architecture

Allocation of resources for the deployment of microservices and their communication has a substantial impact on the performance of the application. In this project, we aim to quantify the effects of using different types of resources: resource type (virtualized or containerized instances, use of functions, and serverless possibility), orchestration, and communication patterns (event-based, API calls, etc.) on application performance.

The main performance metric for this comparison is Latency. You may choose to measure additional metrics to ensure correctness or clarify findings.

The choice of the application (could use a readily available application with proper reference), the platform, and using or not using containerized workloads is at the project group's discretion.

 

(2) Containerized or Serverless Learning & Inference

There are different choices in building your AI application and training and deploying your model, including but not limited to deep learning containers and serverless machine learning frameworks. In this project, you are required to train a model and deploy a machine learning workload of your choice in containerized and serverless environments and evaluate the fit of each deployment choice to your use case.

Note that model performance is not the main measured metric here and the goal is not to use trained model APIs - the goals is to experiment with the deployment choices of cloud resources customized for your use case. You may choose your metrics to ensure correctness or clarify of your findings based on your use case.

The choice of the model, data, and the platform is at the project group's discretion.

 

(3) Implement a fault-tolerant distributed system

Decide a system (e.g., key-value store, or a blockchain system, etc.) identifying key behaviour of your system, its components, and users, and identify the corresponding fault model. Then, design and implement an appropriate mechanism to make your system work despite the type of faults that are possible in that environment (e.g., appropriate consensus algorithm).

Please note that the goal of this project is not implementing an application. This is implementing the back-end system that provides you a fault-tolerant system of your choice.

 

Some useful references:

[1] https://microservices.io/Links to an external site.: This resource contains libraries of sample codes you can use, design exampleLinks to an external site., as well as information about ServerlessLinks to an external site. and Serverful [1Links to an external site., 2Links to an external site.] deployments.
[2] A Google Tutorial on Microservices: https://cloud.google.com/architecture/microservices-architecture-introductionLinks to an external site. 
[3] An AWS Hands-on Tutorial on Breaking a Monolith to Microservices: https://aws.amazon.com/getting-started/hands-on/break-monolith-app-microservices-ecs-docker-ec2/Links to an external site.
[4] Building Microservices, S. Newman (2022) , Chapter Three has a good design example (Book freely available through SFU Library)
[5] A code repository: https://github.com/mspnp/microservices-reference-implementationLinks to an external site.  and deployment scenarios [1Links to an external site., 2Links to an external site.]
[6] This reference is to show you the usefulness of your projects in real-world services: CRISP: Critical Path Analysis of Large-Scale Microservice Architectures: https://www.usenix.org/conference/atc22/presentation/zhang-zhizhouLinks to an external site.
[7] Deep Learning Containers: https://cloud.google.com/deep-learning-containers/docsLinks to an external site.
[8] Serverless Machine Learning: https://aws.amazon.com/blogs/compute/deploying-machine-learning-models-with-serverless-templates/Links to an external site.


Part 2:
The specific problem statement we crafted and submitted

Our project implements a fault-tolerant distributed key-value store using the Raft consensus algorithm. The system consists of multiple server nodes that collectively maintain a replicated state machine, a client interface for read/write operations, and a chaos testing framework for validating fault tolerance. The key-value store ideally supports strongly consistent reads, idempotent writes with duplicate detection, and automatic leader election upon node failure. Our fault model covers crash failures, network partitions (communication loss), message omission and latency, and storage failures. To handle these faults, the Raft protocol ensures that as long as a majority quorum of nodes is alive, the system continues to accept writes, elect new leaders, and replicate data consistently.
We validate these guarantees through systematic chaos testing[1] : killing nodes, freezing processes to simulate partitions, and injecting network delays, then measuring recovery time and verifying data consistency.

We take inspiration from:
- {1} https://github.com/Netflix/chaosmonkey/ 
- [2] https://github.com/rqlite/rqlite

Part 3: Project Progress Report, what the prof asked:
In this project step, you are required to refine your project based on your initial experiments after picking your project title. To do this, you need to start the design and hands-on steps and understand the scope, challenges you may encounter, and shortcomings of your initial thoughts when you decide on your project title.

 

Provide a few lines of description to define your project scope as it has developed within your team since deciding your project title, the challenges you have encountered or challenges you foresee, and your refined and updated plan (since you picked your title) to progress.

To explain your current progress, please submit a one-page file that includes the following (together with the few general lives described above):

(1) Solution Design: Explain your (initial or refined) system design (of all options) including details of the components (10 pts)
     This may include details such as components, communications among components, and reasoning for specific choices.
(2) Implementation: Explain your current implementation state and implementation plan upon completion of your project (10 pts)
      This may include details such as platform, products, and current state of your code (development, reuse, etc.) and deployment.
(3) Evaluation Plan: Explain your evaluation plan for the outcomes of your project (10 pts)
      This may include details such as platforms, tools, methodology, and metrics.

The sections of the project are defined in a general way to be helpful for all project titles. You can name the section(s) based on the details of your project. However, the report needs to demonstrate progress in design, implementation, and planning for the evaluation of your project.

Please note that your grades for each section are allocated for demonstration of making progress, and while we will provide you some formative feedback if there are any mistakes in the current state of your design, no marks will be deducted at this point. However, you are required to correct those mistakes for the final presentation and outcomes submission.

Part 4: Our submission to Project Progress Report
1. Solution Design
Our system has four components, to be deployed across three GCE VMs.
KV-Store Node: The core server with a two-port design, a Raft TCP port for consensus traffic (leader
election, log replication, heartbeats) and a separate gRPC port for client API requests (Get, Set, Delete).
We separated these so we can inject faults on the client port without disrupting Raft internals. Uses the
HashiCorp Raft library with BoltDB for durable log storage, and an in-memory FSM (key-value map)
with per-client sequence numbers for idempotent duplicate detection.
Client CLI: Connects to any node via gRPC; automatically redirects to the leader if it hits a follower.
Chaos Proxy: A TCP proxy that drops connections or adds latency on the gRPC port, simulating network
faults without affecting Raft consensus.
Writes flow: Client → Leader (gRPC) → AppendEntries to followers (Raft TCP) → quorum commit →
FSM apply → response. Reads go via leader with VerifyLeader() for linearizable consistency. This is a
CP system per CAP, the minority side of a partition becomes unavailable rather than serving stale data.
2. Implementation
Stack: Go, gRPC/Protobuf, HashiCorp Raft v1.7.3, BoltDB. Deployment via Dockerfile and a
gcloud CLI script that provisions VMs, configures firewall rules, and uploads binaries.
Done so far: The core node is working (leader election, log replication, snapshotting, graceful
leadership transfer). The gRPC API, chaos proxy, and dashboard are implemented. We have
FSM unit tests and have started writing bash test scripts for Phase 1 (liveness/election) and Phase
2 (network partitions) that work locally against the dashboard API.
Remaining: Deploy to GCP and verify cluster formation across VMs, tune Raft timeouts for real
latency, write Phase 3 (latency faults) and Phase 4 (durability) test scripts, adapt existing scripts
to use VM IPs instead of localhost, and run the full evaluation.
3. Evaluation Plan
We’ll evaluate across four phases on the GCP cluster via bash test scripts and the kv-client CLI
Phase 1 - Liveness/Election: Kill leader via SSH, measure MTTR to new election via Health
RPC polling. Verify quorum loss correctly blocks writes.
Phase 2 - Network Partitions: SIGSTOP/SIGCONT to freeze nodes. Verify no split-brain on
minority partition, leader step-down on majority partition, and log catch-up after healing.
Phase 3 - Resource/Latency: Chaos proxy with 2s follower delay, 500ms leader delay, 50%
packet loss (partial success, all acks durable).
Phase 4 - Durability: Kill all nodes and restart, verify all keys recovered from BoltDB. Kill
leader mid-write and verify zero acknowledged-write loss.
Key metrics: MTTR (seconds), write throughput (ops/sec) under faults, and key recovery ratio
(target: 100% for acknowledged writes). Results will compare localhost vs. GCP behavior.

Part 5: Prof feedback on our project progress report (note in our case there is less feedback on the project itself and more on the phrasing in our report which could have been better)

Thank you so much for your submission.

I like that you chose this project. I believe it includes a lot of learning.

It seems four component expected, but three are listed. Is anything missing? (Or did I miss it?)

If you have a “core server” which sounds very centralized, where are your replicated state machines?

Implementation plan phases sound good.

What is the expected number of nodes? What system considerations led you to this number?

While we do not deduct marks at this point, if this was a graded element beyond the availability of submission and informative feedback, grades would be 810, 8/10, and 8/10 for each of the solution design, implementation, and evaluation sections, respectively.


Thanks again, and I am looking forward to your project presentation and outcomes.


Part 6: Upcoming presentation
In this step of the project, each group will present their work to the class on April 2, 2026 and April 9, 2026, during class time.

 

Deliverables
In this step of the project the following deliverables are required:

Submission of pdf of group presentation slides
Presentation to the class
Peer-review other groups' presentations (individual assignment)
Submission of project outcomes
 

Presentation
You need to present your project in five Minutes:

Your Project Title + Name of the Team Members (~30 Seconds)
Your System's Functionality and Design (~90-120 Seconds)
Your Implementation Details (~60-90 Seconds)
You Measurement Results and Explanation of The Results (~60-90 Seconds)
Each presentation will follow with ~2-3 minutes to answer 2-3 question from peer reviewers (peer review process discussed below). Please note we have limited time for presentations and if we do not strictly follow the presentation plan, we may not be see all group presentations.

 

Presentation slides (to be submitted prior to the presentation), should only include:

Title & description of your project
Brief description of purpose of your system and current functionalities
Your technical final design (as you see appropriate for a presentation)
Brief description of results, and technical challenges in your system in achieving them
(E.g., metrics chosen, reasoning, challenges in meeting your goals including your measurements, and improvements, etc. If you did not have challenges, please just show the achieved results with measurements)
Your presentations should include total of four (4) pages.

 

Peer Review
On the presentation day, please show your work and talk about your project to the class. Each presentation will be peer-reviewed by classmates using the following criteria:

Grading Criteria	Mark	Grading Guideline
System Design	3	0: Missing system design or design with major flaws
0.5-1.5: Partially correct design
2-2.5: Correct design with minors mistakes
3: Correct and clear design (for all options)
Implementation	3	0: Missing implementation o major implementation flaws
0.5-1.5: Partially correct implementation
2-2.5: Correct implementation with minor mistakes
3: Perfect implementation
Results	3	0: Missing results or major flaw in results
0.5-1.5: Correct results without explanation
2-2.5: Correct results with minor mistakes in explanations
3: Expected results and clear reasoning
Clarity of Presentation	3	0: Vague or unstructured information
0.5-1.5: Missing needed main information or structure of presentation
2-2.5: Correct and clear presentation flow and explanations with minor mistakes
3: Clear and articulate presentation
Timeliness	3	0: 90+ seconds of delay in start or presentation 90+ seconds longer than expected
1-1.5: 45-90 seconds of delay in start or presentation 45-90 seconds longer than expected
2-2.5: 15-45 seconds of delay in start or presentation 15-45 seconds longer than expected
3: 0-15 seconds of delay in start or presentation 0-15 seconds longer than expected
As the deadline to submit the slides is April 2, 2026, 9:00 AM, the submission will be available at this time as the reference for grading during presentations. You have to enter your feedback as comments and provide mark on Canvas in the review process.

Note that the grades for presentations peer reviews are out of 15 points. The last 5 points will be provided after all reviews assigned to each person are finished by April 9, 2026, 11:59pm (no late submissions).

During the feedback process, please also choose:

Best Project (System Functionality, System Design and Implementation, Results and Interpretation) - 3 bonus points
Best Presentation (Timing, Technical Content, Delivery) - 2 bonus points
You can use this link (will become available on the day of presentations) to vote for your favourite teams (link accessible only during presentation times). These bonus points are from the 25 total bonus points for 2% of total grade.

Peer review means classmates will see each others' presentation submission. Please remove any sensitive information from your submission if you do not feel comfortable to be seen by a classmate.

Your reviews are NOT anonymous. Grade and  comments are visible to classmate (with reviewer name). Please be specific and detailed, as well as kind, respectful, and considerate.

 

Submission
Presentations: Please submit the pdf file of project presentation slides before the presentations (before) to this assignment on Canvas.

Outcomes: Please submit the zip file of project outcomes (before) to outcomes assignment on Canvas.

Note (1): Due to scheduled peer-review, this step of the project does not have any late submissions.

Note (2): Please ask your clarifying questions regrading Project Presentations in class on Monday (March 30th), and details in this page will be edited to reflect the answers if necessary.


Part 7: Please submit a pdf file of your report that includes your final report for your project.

Your final report needs to include:

Your project title (Project 1, 2, or 3)
Your system design choices (e.g., choice of microservices, communication, distributed storage, etc.)
Your implementation details (platform, technology choices, tools, etc.)
Your results and measurements (measurement methods, results, etc.)
Analysis of your results (general results, argument of correctness, highlighting interesting results, etc.)
Please format your report having the following in mind:

Please do not include print screenshots of pages or dashboards of visualizations in your report
Please keep your report within a maximum of 3 pages including all figures, and a maximum of 1000 words
Please try to be brief but provide all needed details
If you need to submit any additional files (code, deployment setups, etc.) please use github and provide the link in your report.

 

Grading Criteria
System design (10 pts): How well the distributed system that solves the presented problem is designed.
Implementation (10 pts): How complete is the implementation, how correctly does it map to the system design, and how much the system implementation shows correct use of distributed system design concepts.
Testing tools, methodology and results (5 pts): Tools used, testing methodology, and correctness of results.
Depth of experiment and analysis (10 pts): Depth of understanding and analysis of the results
Writing and presentation (5 pts): Articulation, clarity, brevity, and structure of the writing and presentation

In each category grades are: 0-10%: Weak, 10-40%: Acceptable, 40-60%: Good, 60-80%: Very Good, 80-100%: Excellent

Please ask your clarifying questions regarding your Project under this discussion post, and details on this page will be edited to reflect the answers if necessary.
