/*
 * Copyright 2019-2020, Offchain Labs, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/* eslint-env node, mocha */
import { ethers, run } from "hardhat";
import { Signer } from "@ethersproject/abstract-signer";
import { BigNumberish, BigNumber } from "@ethersproject/bignumber";
import { BytesLike, hexConcat, zeroPad } from "@ethersproject/bytes";
import { ContractTransaction } from "@ethersproject/contracts";
import { assert, expect } from "chai";
import {
  BridgeCreator__factory,
  ChallengeManager,
  ChallengeManager__factory,
  OneStepProofEntry__factory,
  OneStepProver0__factory,
  OneStepProverHostIo__factory,
  OneStepProverMath__factory,
  OneStepProverMemory__factory,
  RollupAdminLogic,
  RollupAdminLogic__factory,
  RollupCreator__factory,
  RollupUserLogic,
  RollupUserLogic__factory,
  SequencerInbox,
  SequencerInbox__factory,
} from "../../build/types";
import { initializeAccounts } from "./utils";

import { Node, RollupContract, forceCreateNode, assertionEquals } from "./common/rolluplib";
import { RollupLib } from "../../build/types/src/rollup/RollupUserLogic.sol/RollupUserLogic";
type AssertionStruct = RollupLib.AssertionStruct;
type ExecutionStateStruct = RollupLib.ExecutionStateStruct;
import { keccak256 } from "ethers/lib/utils";
import { ConfigStruct, RollupCreatedEvent } from "../../build/types/src/rollup/RollupCreator";
import { constants } from "ethers";
import { blockStateHash, MachineStatus } from "./common/challengeLib";
import * as globalStateLib from "./common/globalStateLib";
import { RollupChallengeStartedEvent } from "../../build/types/src/rollup/IRollupCore";

const zerobytes32 = ethers.constants.HashZero;
const stakeRequirement = 10;
const stakeToken = ethers.constants.AddressZero;
const confirmationPeriodBlocks = 100;
const minimumAssertionPeriod = 75;
const ZERO_ADDR = ethers.constants.AddressZero;
const extraChallengeTimeBlocks = 20;
const wasmModuleRoot = "0x9900000000000000000000000000000000000000000000000000000000000010";

// let rollup: RollupContract
let rollup: RollupContract;
let rollupUser: RollupUserLogic;
let rollupAdmin: RollupAdminLogic;
let accounts: Signer[];
let validators: Signer[];
let sequencerInbox: SequencerInbox;
let admin: Signer;
let sequencer: Signer;
let challengeManager: ChallengeManager;

async function getDefaultConfig(
  _confirmPeriodBlocks = confirmationPeriodBlocks
): Promise<ConfigStruct> {
  return {
    baseStake: stakeRequirement,
    chainId: stakeToken,
    confirmPeriodBlocks: _confirmPeriodBlocks,
    extraChallengeTimeBlocks: extraChallengeTimeBlocks,
    owner: await accounts[0].getAddress(),
    sequencerInboxMaxTimeVariation: {
      delayBlocks: (60 * 60 * 24) / 15,
      futureBlocks: 12,
      delaySeconds: 60 * 60 * 24,
      futureSeconds: 60 * 60,
    },
    stakeToken: stakeToken,
    wasmModuleRoot: wasmModuleRoot,
    loserStakeEscrow: ZERO_ADDR,
  };
}

const setup = async () => {
  const accounts = await initializeAccounts();
  admin = accounts[0];

  const user = accounts[1];

  const val1 = accounts[2];
  const val2 = accounts[3];
  const val3 = accounts[4];
  const val4 = accounts[5];
  sequencer = accounts[6];

  const oneStep0Fac = (await ethers.getContractFactory(
    "OneStepProver0"
  )) as OneStepProver0__factory;
  const oneStep0 = await oneStep0Fac.deploy();
  const oneStepMemoryFac = (await ethers.getContractFactory(
    "OneStepProverMemory"
  )) as OneStepProverMemory__factory;
  const oneStepMemory = await oneStepMemoryFac.deploy();
  const oneStepMathFac = (await ethers.getContractFactory(
    "OneStepProverMath"
  )) as OneStepProverMath__factory;
  const oneStepMath = await oneStepMathFac.deploy();
  const oneStepHostIoFac = (await ethers.getContractFactory(
    "OneStepProverHostIo"
  )) as OneStepProverHostIo__factory;
  const oneStepHostIo = await oneStepHostIoFac.deploy();

  const oneStepProofEntryFac = (await ethers.getContractFactory(
    "OneStepProofEntry"
  )) as OneStepProofEntry__factory;
  const oneStepProofEntry = await oneStepProofEntryFac.deploy(
    oneStep0.address,
    oneStepMemory.address,
    oneStepMath.address,
    oneStepHostIo.address
  );

  const challengeManagerTemplateFac = (await ethers.getContractFactory(
    "ChallengeManager"
  )) as ChallengeManager__factory;
  const challengeManagerTemplate = await challengeManagerTemplateFac.deploy();

  const rollupAdminLogicFac = (await ethers.getContractFactory(
    "RollupAdminLogic"
  )) as RollupAdminLogic__factory;
  const rollupAdminLogicTemplate = await rollupAdminLogicFac.deploy();

  const rollupUserLogicFac = (await ethers.getContractFactory(
    "RollupUserLogic"
  )) as RollupUserLogic__factory;
  const rollupUserLogicTemplate = await rollupUserLogicFac.deploy();

  const bridgeCreatorFac = (await ethers.getContractFactory(
    "BridgeCreator"
  )) as BridgeCreator__factory;
  const bridgeCreator = await bridgeCreatorFac.deploy();

  const rollupCreatorFac = (await ethers.getContractFactory(
    "RollupCreator"
  )) as RollupCreator__factory;
  const rollupCreator = await rollupCreatorFac.deploy();

  await rollupCreator.setTemplates(
    bridgeCreator.address,
    oneStepProofEntry.address,
    challengeManagerTemplate.address,
    rollupAdminLogicTemplate.address,
    rollupUserLogicTemplate.address
  );

  const nonce = await rollupCreator.signer.provider!.getTransactionCount(rollupCreator.address);
  const expectedRollupAddress = ethers.utils.getContractAddress({
    from: rollupCreator.address,
    nonce: nonce + 2,
  });

  const response = await rollupCreator.createRollup(
    await getDefaultConfig(),
    expectedRollupAddress
  );
  const rec = await response.wait();

  const rollupCreatedEvent = rollupCreator.interface.parseLog(rec.logs[rec.logs.length - 1])
    .args as RollupCreatedEvent["args"];

  const rollupAdmin = rollupAdminLogicFac
    .attach(expectedRollupAddress)
    .connect(rollupCreator.signer);
  const rollupUser = rollupUserLogicFac.attach(expectedRollupAddress).connect(user);

  await rollupAdmin.setValidator(
    [await val1.getAddress(), await val2.getAddress(), await val3.getAddress()],
    [true, true, true]
  );

  sequencerInbox = ((await ethers.getContractFactory(
    "SequencerInbox"
  )) as SequencerInbox__factory).attach(rollupCreatedEvent.sequencerInbox);

  await sequencerInbox.setIsBatchPoster(await sequencer.getAddress(), true);

  challengeManager = ((await ethers.getContractFactory(
    "ChallengeManager"
  )) as ChallengeManager__factory).attach(await rollupUser.challengeManager());

  return {
    admin,
    user,

    rollupAdmin,
    rollupUser,

    validators: [val1, val2, val3, val4],

    rollupAdminLogicTemplate,
    rollupUserLogicTemplate,
    blockChallengeFactory: challengeManagerTemplateFac,
    rollupEventBridge: await rollupAdmin.rollupEventBridge(),
    outbox: await rollupAdmin.outbox(),
    sequencerInbox: rollupCreatedEvent.sequencerInbox,
    delayedBridge: rollupCreatedEvent.delayedBridge,
  };
};

async function tryAdvanceChain(blocks: number): Promise<void> {
  try {
    for (let i = 0; i < blocks; i++) {
      await ethers.provider.send("evm_mine", []);
    }
  } catch (e) {
    // EVM mine failed. Try advancing the chain by sending txes if the node
    // is in dev mode and mints blocks when txes are sent
    for (let i = 0; i < blocks; i++) {
      const tx = await accounts[0].sendTransaction({
        value: 0,
        to: await accounts[0].getAddress(),
      });
      await tx.wait();
    }
  }
}

async function advancePastAssertion(blockProposed: number): Promise<void> {
  await tryAdvanceChain(blockProposed + confirmationPeriodBlocks);
}

function newRandomExecutionState() {
  const blockHash = keccak256(ethers.utils.randomBytes(32));
  const sendRoot = keccak256(ethers.utils.randomBytes(32));
  const machineStatus = 1;

  return newExecutionState(blockHash, sendRoot, 1, 0, machineStatus);
}

function newExecutionState(
  blockHash: string,
  sendRoot: string,
  inboxPosition: BigNumberish,
  positionInMessage: BigNumberish,
  machineStatus: BigNumberish
): ExecutionStateStruct {
  return {
    globalState: {
      bytes32Vals: [blockHash, sendRoot],
      u64Vals: [inboxPosition, positionInMessage],
    },
    machineStatus,
  };
}

function newRandomAssertion(prevExecutionState: ExecutionStateStruct): AssertionStruct {
  return {
    beforeState: prevExecutionState,
    afterState: newRandomExecutionState(),
    numBlocks: 10,
  };
}

async function makeSimpleNode(
  rollup: RollupContract,
  sequencerInbox: SequencerInbox,
  parentNode: {
    assertion: { afterState: ExecutionStateStruct };
    nodeNum: number;
    nodeHash: BytesLike;
    inboxMaxCount: BigNumber;
  },
  siblingNode?: Node,
  prevNode?: Node,
  stakeToAdd?: BigNumber
): Promise<{ tx: ContractTransaction; node: Node }> {
  const staker = await rollup.rollup.getStaker(await rollup.rollup.signer.getAddress());

  const assertion = newRandomAssertion(parentNode.assertion.afterState);
  const { tx, node, expectedNewNodeHash } = await rollup.stakeOnNewNode(
    sequencerInbox,
    parentNode,
    assertion,
    siblingNode,
    stakeToAdd
  );

  expect(assertionEquals(assertion, node.assertion), "unexpected assertion").to.be.true;
  assert.equal(node.nodeNum, (prevNode || siblingNode || parentNode).nodeNum + 1);
  assert.equal(node.nodeHash, expectedNewNodeHash);

  if (stakeToAdd) {
    const stakerAfter = await rollup.rollup.getStaker(await rollup.rollup.signer.getAddress());
    expect(stakerAfter.latestStakedNode.toNumber()).to.eq(node.nodeNum);
    expect(stakerAfter.amountStaked.toString()).to.eq(
      staker.amountStaked.add(stakeToAdd).toString()
    );
  }
  return { tx, node };
}

const makeSends = (count: number, batchStart = 0) => {
  return [...Array(count)].map((_, i) =>
    hexConcat([[0], zeroPad([i + batchStart], 32), zeroPad([0], 32), zeroPad([1], 32)])
  );
};

let prevNode: Node;
const prevNodes: Node[] = [];

function updatePrevNode(node: Node) {
  prevNode = node;
  prevNodes.push(node);
}

describe("ArbRollup", () => {
  it("should deploy contracts", async function () {
    accounts = await initializeAccounts();

    await run("deploy", { tags: "test" });
  });

  it("should initialize", async function () {
    const {
      rollupAdmin: rollupAdminContract,
      rollupUser: rollupUserContract,
      user: userI,
      admin: adminI,
      validators: validatorsI,
    } = await setup();
    rollupAdmin = rollupAdminContract;
    rollupUser = rollupUserContract;
    admin = adminI;
    validators = validatorsI;
    rollup = new RollupContract(rollupUser.connect(validators[0]));
  });

  it("should only initialize once", async function () {
    await expect(
      rollupAdmin.initialize(await getDefaultConfig(), {
        challengeManager: constants.AddressZero,
        delayedBridge: constants.AddressZero,
        outbox: constants.AddressZero,
        rollupAdminLogic: constants.AddressZero,
        rollupEventBridge: constants.AddressZero,
        rollupUserLogic: constants.AddressZero,
        sequencerInbox: constants.AddressZero,
      })
    ).to.be.revertedWith("Initializable: contract is already initialized");
  });

  it("should place stake on new node", async function () {
    await tryAdvanceChain(minimumAssertionPeriod);

    const initNode: {
      assertion: { afterState: ExecutionStateStruct };
      nodeNum: number;
      nodeHash: BytesLike;
      inboxMaxCount: BigNumber;
    } = {
      assertion: {
        afterState: {
          globalState: {
            bytes32Vals: [zerobytes32, zerobytes32],
            u64Vals: [0, 0],
          },
          machineStatus: MachineStatus.FINISHED,
        },
      },
      inboxMaxCount: BigNumber.from(1),
      nodeHash: zerobytes32,
      nodeNum: 0,
    };

    const stake = await rollup.currentRequiredStake();
    const { node } = await makeSimpleNode(
      rollup,
      sequencerInbox,
      initNode,
      undefined,
      undefined,
      stake
    );
    updatePrevNode(node);
  });

  it("should let a new staker place on existing node", async function () {
    const stake = await rollup.currentRequiredStake();
    await rollupUser
      .connect(validators[2])
      .newStakeOnExistingNode(1, prevNode.nodeHash, { value: stake });
  });

  it("should move stake to a new node", async function () {
    await tryAdvanceChain(minimumAssertionPeriod);
    const { node } = await makeSimpleNode(rollup, sequencerInbox, prevNode);
    updatePrevNode(node);
  });

  it("should let the second staker place on the new node", async function () {
    await rollup.connect(validators[2]).stakeOnExistingNode(2, prevNode.nodeHash);
  });

  it("should confirm node", async function () {
    await tryAdvanceChain(confirmationPeriodBlocks * 2);

    await rollup.confirmNextNode(prevNodes[0]);
  });

  it("should confirm next node", async function () {
    await tryAdvanceChain(minimumAssertionPeriod);
    await rollup.confirmNextNode(prevNodes[1]);
  });

  let challengedNode: Node;
  let validNode: Node;
  it("should let the first staker make another node", async function () {
    await tryAdvanceChain(minimumAssertionPeriod);
    const { node } = await makeSimpleNode(rollup, sequencerInbox, prevNode);
    challengedNode = node;
    validNode = node;
  });

  let challengerNode: Node;
  it("should let the second staker make a conflicting node", async function () {
    await tryAdvanceChain(minimumAssertionPeriod);
    const { node } = await makeSimpleNode(
      rollup.connect(validators[2]),
      sequencerInbox,
      prevNode,
      validNode
    );
    challengerNode = node;
  });

  it("should fail to confirm first staker node", async function () {
    await advancePastAssertion(challengedNode.proposedBlock);
    await expect(rollup.confirmNextNode(validNode)).to.be.revertedWith("NOT_ALL_STAKED");
  });

  let challengeIndex: number;
  it("should initiate a challenge", async function () {
    const tx = rollup.createChallenge(
      await validators[0].getAddress(),
      await validators[2].getAddress(),
      challengedNode,
      challengerNode
    );
    const receipt = await (await tx).wait();
    const ev = rollup.rollup.interface.parseLog(receipt.logs![receipt.logs!.length - 1]);
    expect(ev.name).to.equal("RollupChallengeStarted");

    const parsedEv = ev.args as RollupChallengeStartedEvent["args"];
    challengeIndex = parsedEv.challengeIndex.toNumber();
  });

  it("should make a new node", async function () {
    const { node } = await makeSimpleNode(
      rollup,
      sequencerInbox,
      validNode,
      undefined,
      challengerNode
    );
    challengedNode = node;
  });

  it("new staker should make a conflicting node", async function () {
    const stake = await rollup.currentRequiredStake();
    await rollup.rollup
      .connect(validators[1])
      .newStakeOnExistingNode(3, validNode.nodeHash, { value: stake.add(50) });

    const { node } = await makeSimpleNode(
      rollup.connect(validators[1]),
      sequencerInbox,
      validNode,
      challengedNode
    );
    challengerNode = node;
  });

  it("asserter should win via timeout", async function () {
    await advancePastAssertion(challengedNode.proposedBlock);
    await challengeManager.connect(validators[0]).timeout(challengeIndex);
  });

  it("confirm first staker node", async function () {
    await rollup.confirmNextNode(validNode);
  });

  it("should reject out of order second node", async function () {
    await rollup.rejectNextNode(stakeToken);
  });

  it("should initiate another challenge", async function () {
    const tx = rollup.createChallenge(
      await validators[0].getAddress(),
      await validators[1].getAddress(),
      challengedNode,
      challengerNode
    );
    const receipt = await (await tx).wait();
    const ev = rollup.rollup.interface.parseLog(receipt.logs![receipt.logs!.length - 1]);
    expect(ev.name).to.equal("RollupChallengeStarted");
    const parsedEv = ev.args as RollupChallengeStartedEvent["args"];
    challengeIndex = parsedEv.challengeIndex.toNumber();

    await expect(
      rollup.rollup.completeChallenge(
        challengeIndex,
        await sequencer.getAddress(),
        await validators[3].getAddress()
      )
    ).to.be.revertedWith("WRONG_SENDER");
  });

  it("challenger should reply in challenge", async function () {
    const seg0 = blockStateHash(
      BigNumber.from(challengerNode.assertion.beforeState.machineStatus),
      globalStateLib.hash(challengerNode.assertion.beforeState.globalState)
    );

    const seg1 = blockStateHash(
      BigNumber.from(challengedNode.assertion.afterState.machineStatus),
      globalStateLib.hash(challengedNode.assertion.afterState.globalState)
    );
    await challengeManager.connect(validators[1]).bisectExecution(
      challengeIndex,
      {
        challengePosition: BigNumber.from(0),
        oldSegments: [seg0, seg1],
        oldSegmentsLength: BigNumber.from(challengedNode.assertion.numBlocks),
        oldSegmentsStart: 0,
      },
      [
        seg0,
        zerobytes32,
        zerobytes32,
        zerobytes32,
        zerobytes32,
        zerobytes32,
        zerobytes32,
        zerobytes32,
        zerobytes32,
        zerobytes32,
        zerobytes32,
      ]
    );
  });

  it("challenger should win via timeout", async function () {
    await advancePastAssertion(challengedNode.proposedBlock);
    await challengeManager.timeout(challengeIndex);
  });

  it("should reject out of order second node", async function () {
    await rollup.rejectNextNode(await validators[1].getAddress());
  });

  it("confirm next node", async function () {
    await tryAdvanceChain(confirmationPeriodBlocks);
    await rollup.confirmNextNode(challengerNode);
  });

  it("should add and remove stakes correctly", async function () {
    /*
      RollupUser functions that alter stake and their respective Core logic

      user: newStake
      core: createNewStake

      user: addToDeposit
      core: increaseStakeBy

      user: reduceDeposit
      core: reduceStakeTo

      user: returnOldDeposit
      core: withdrawStaker

      user: withdrawStakerFunds
      core: withdrawFunds
    */

    const initialStake = await rollup.rollup.amountStaked(await validators[1].getAddress());

    await rollup.connect(validators[1]).reduceDeposit(initialStake);

    await expect(
      rollup.connect(validators[1]).reduceDeposit(initialStake.add(1))
    ).to.be.revertedWith("TOO_LITTLE_STAKE");

    await rollup
      .connect(validators[1])
      .addToDeposit(await validators[1].getAddress(), { value: 5 });

    await rollup.connect(validators[1]).reduceDeposit(5);

    const prevBalance = await validators[1].getBalance();
    const prevWithdrawablefunds = await rollup.rollup.withdrawableFunds(
      await validators[1].getAddress()
    );

    const tx = await rollup.rollup
      .connect(validators[1])
      .withdrawStakerFunds();
    const receipt = await tx.wait();
    const gasPaid = receipt.gasUsed.mul(receipt.effectiveGasPrice);

    const postBalance = await validators[1].getBalance();
    const postWithdrawablefunds = await rollup.rollup.withdrawableFunds(
      await validators[1].getAddress()
    );

    expect(postWithdrawablefunds).to.equal(0);
    expect(postBalance.add(gasPaid)).to.equal(prevBalance.add(prevWithdrawablefunds));

    // this gets deposit and removes staker
    await rollup.rollup.connect(validators[1]).returnOldDeposit(await validators[1].getAddress());
    // all stake is now removed
  });

  it("should allow removing zombies", async function () {
    const zombieCount = (await rollup.rollup.connect(validators[2]).zombieCount()).toNumber();
    for (let i = 0; i < zombieCount; i++) {
      await rollup.rollup.connect(validators[2]).removeZombie(0, 1024);
    }
  });

  it("should pause the contracts then resume", async function () {
    const prevIsPaused = await rollup.rollup.paused();
    expect(prevIsPaused).to.equal(false);

    await rollupAdmin.pause();

    const postIsPaused = await rollup.rollup.paused();
    expect(postIsPaused).to.equal(true);

    await expect(
      rollup.connect(validators[1]).addToDeposit(await validators[1].getAddress(), { value: 5 })
    ).to.be.revertedWith("Pausable: paused");

    await rollupAdmin.resume();
  });

  it("should allow admin to alter rollup while paused", async function () {
    const prevLatestConfirmed = await rollup.rollup.latestConfirmed();
    expect(prevLatestConfirmed.toNumber()).to.equal(6);
    // prevNode is prevLatestConfirmed
    prevNode = challengerNode;

    const stake = await rollup.currentRequiredStake();

    const { node: node1 } = await makeSimpleNode(
      rollup,
      sequencerInbox,
      prevNode,
      undefined,
      undefined,
      stake
    );
    const node1Num = await rollup.rollup.latestNodeCreated();
    expect(node1Num.toNumber(), "node1num").to.eq(node1.nodeNum);

    await tryAdvanceChain(minimumAssertionPeriod);

    const { node: node2 } = await makeSimpleNode(
      rollup.connect(validators[2]),
      sequencerInbox,
      prevNode,
      node1,
      undefined,
      stake
    );
    const node2Num = await rollup.rollup.latestNodeCreated();
    expect(node2Num.toNumber(), "node2num").to.eq(node2.nodeNum);

    const tx = await rollup.createChallenge(
      await validators[0].getAddress(),
      await validators[2].getAddress(),
      node1,
      node2
    );
    const receipt = await tx.wait();
    const ev = rollup.rollup.interface.parseLog(receipt.logs![receipt.logs!.length - 1]);
    expect(ev.name).to.equal("RollupChallengeStarted");
    const parsedEv = ev.args as RollupChallengeStartedEvent["args"];
    challengeIndex = parsedEv.challengeIndex.toNumber();

    expect(await challengeManager.currentResponder(challengeIndex), "turn challenger").to.eq(
      await validators[2].getAddress()
    );

    await expect(
      rollupAdmin.forceResolveChallenge(
        [await validators[0].getAddress()],
        [await validators[2].getAddress()]
      ),
      "force resolve"
    ).to.be.revertedWith("Pausable: not paused");

    await expect(
      rollup.createChallenge(
        await validators[0].getAddress(),
        await validators[2].getAddress(),
        node1,
        node2
      ),
      "create challenge"
    ).to.be.revertedWith("IN_CHAL");

    await rollupAdmin.pause();

    await rollupAdmin.forceResolveChallenge(
      [await validators[0].getAddress()],
      [await validators[2].getAddress()]
    );

    // challenge should have been destroyed
    expect(await challengeManager.currentResponder(challengeIndex), "turn reset").to.equal(
      constants.AddressZero
    );

    const challengeA = await rollupAdmin.currentChallenge(await validators[0].getAddress());
    const challengeB = await rollupAdmin.currentChallenge(await validators[2].getAddress());

    expect(challengeA).to.equal(ZERO_ADDR);
    expect(challengeB).to.equal(ZERO_ADDR);

    await rollupAdmin.forceRefundStaker([
      await validators[0].getAddress(),
      await validators[2].getAddress(),
    ]);

    const adminAssertion = newRandomAssertion(prevNode.assertion.afterState);
    const { node: forceCreatedNode1 } = await forceCreateNode(
      rollupAdmin,
      sequencerInbox,
      prevNode,
      adminAssertion,
      node2
    );
    expect(assertionEquals(forceCreatedNode1.assertion, adminAssertion), "assertion error").to.be
      .true;

    const adminNodeNum = await rollup.rollup.latestNodeCreated();
    const midLatestConfirmed = await rollup.rollup.latestConfirmed();
    expect(midLatestConfirmed.toNumber()).to.equal(6);

    expect(adminNodeNum.toNumber()).to.equal(node2Num.toNumber() + 1);

    const adminAssertion2 = newRandomAssertion(prevNode.assertion.afterState);
    const { node: forceCreatedNode2 } = await forceCreateNode(
      rollupAdmin,
      sequencerInbox,
      prevNode,
      adminAssertion2,
      forceCreatedNode1
    );

    const postLatestCreated = await rollup.rollup.latestNodeCreated();

    await rollupAdmin.forceConfirmNode(
      adminNodeNum,
      adminAssertion.afterState.globalState.bytes32Vals[0],
      adminAssertion.afterState.globalState.bytes32Vals[1]
    );

    const postLatestConfirmed = await rollup.rollup.latestConfirmed();
    expect(postLatestCreated).to.equal(adminNodeNum.add(1));
    expect(postLatestConfirmed).to.equal(adminNodeNum);

    await rollupAdmin.resume();

    // should create node after resuming

    prevNode = forceCreatedNode1;

    await tryAdvanceChain(minimumAssertionPeriod);

    await expect(
      makeSimpleNode(
        rollup.connect(validators[2]),
        sequencerInbox,
        prevNode,
        undefined,
        forceCreatedNode2,
        stake
      )
    ).to.be.revertedWith("STAKER_IS_ZOMBIE");

    await expect(
      makeSimpleNode(rollup.connect(validators[2]), sequencerInbox, prevNode)
    ).to.be.revertedWith("NOT_STAKED");

    await rollup.rollup.connect(validators[2]).removeOldZombies(0);

    await makeSimpleNode(
      rollup.connect(validators[2]),
      sequencerInbox,
      prevNode,
      undefined,
      forceCreatedNode2,
      stake
    );
  });

  it("should initialize a fresh rollup", async function () {
    const {
      rollupAdmin: rollupAdminContract,
      rollupUser: rollupUserContract,
      user: userI,
      admin: adminI,
      validators: validatorsI,
    } = await setup();
    rollupAdmin = rollupAdminContract;
    rollupUser = rollupUserContract;
    admin = adminI;
    validators = validatorsI;
    rollup = new RollupContract(rollupUser.connect(validators[0]));
  });

  it("should stake on initial node again", async function () {
    await tryAdvanceChain(minimumAssertionPeriod);

    const initNode: {
      assertion: { afterState: ExecutionStateStruct };
      nodeNum: number;
      nodeHash: BytesLike;
      inboxMaxCount: BigNumber;
    } = {
      assertion: {
        afterState: {
          globalState: {
            bytes32Vals: [zerobytes32, zerobytes32],
            u64Vals: [0, 0],
          },
          machineStatus: MachineStatus.FINISHED,
        },
      },
      inboxMaxCount: BigNumber.from(1),
      nodeHash: zerobytes32,
      nodeNum: 0,
    };

    const stake = await rollup.currentRequiredStake();
    const { node } = await makeSimpleNode(
      rollup,
      sequencerInbox,
      initNode,
      undefined,
      undefined,
      stake
    );
    updatePrevNode(node);
  });
});
